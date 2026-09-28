// Tests of the npm shim (bin/langfuse-mcp.js, spec #119, ticket #123), run with `node --test
// packaging/npm/shim.test.mjs` by the release workflow's smoke-npx job on Linux, macOS and Windows.
//
// Each test lays out an installed package the way npm does (node_modules/langfuse-api-mcp with the
// shim, node_modules/langfuse-api-mcp-<platform>-<arch> with the binary) and runs the shim as a
// process. The "binary" is a copy of the running node executable: the shim passes its arguments
// unchanged, so the first argument names the fake binary's script (fake-binary.cjs, written below),
// which reports what it received. Nothing we own is faked; only the Go binary is replaced.
import { test } from "node:test";
import assert from "node:assert/strict";
import { spawn, spawnSync } from "node:child_process";
import { copyFileSync, mkdirSync, mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const shimSource = join(dirname(fileURLToPath(import.meta.url)), "bin", "langfuse-mcp.js");
const windows = process.platform === "win32";

// A value the shell would rewrite in every way it can: command substitution, variables, globs,
// separators, redirections, quotes, escapes, cmd.exe's %VAR%, ^ and !, a newline and a space.
const hostile = "a b;$(echo pwned)`id`|&>out<in *?~ $HOME %PATH% ^! 'q' \"dq\" \\ \n#end";

// The fake binary: echo mode prints what it received as JSON and exits with FAKE_EXIT; wait mode
// prints "ready <pid>" and waits for a signal, exiting 3 on SIGTERM when FAKE_TRAP is set.
const fakeBinaryScript = `
const mode = process.env.FAKE_MODE;
if (mode === "wait") {
  if (process.env.FAKE_TRAP) process.on("SIGTERM", () => process.exit(3));
  process.stdout.write("ready " + process.pid + "\\n");
  setInterval(() => {}, 1000);
} else {
  let stdin = "";
  process.stdin.on("data", (d) => { stdin += d; });
  process.stdin.on("end", () => {
    process.stdout.write(JSON.stringify({ argv: process.argv.slice(2), value: process.env.SMOKE_VALUE, stdin }));
    process.exit(Number(process.env.FAKE_EXIT || 0));
  });
}
`;

// install lays out the main package and, unless withoutPlatformPackage, the platform package for
// this machine, and returns the shim's path and the fake binary's script.
function install(t, { withoutPlatformPackage = false } = {}) {
  const root = mkdtempSync(join(tmpdir(), "langfuse-mcp-shim-"));
  t.after(() => rmSync(root, { recursive: true, force: true }));
  const modules = join(root, "node_modules");
  const shim = join(modules, "langfuse-api-mcp", "bin", "langfuse-mcp.js");
  mkdirSync(dirname(shim), { recursive: true });
  copyFileSync(shimSource, shim);
  writeFileSync(join(modules, "langfuse-api-mcp", "package.json"), JSON.stringify({ name: "langfuse-api-mcp", version: "0.0.0" }));
  if (!withoutPlatformPackage) {
    const pkg = join(modules, `langfuse-api-mcp-${process.platform}-${process.arch}`);
    mkdirSync(join(pkg, "bin"), { recursive: true });
    writeFileSync(join(pkg, "package.json"), JSON.stringify({ name: `langfuse-api-mcp-${process.platform}-${process.arch}`, version: "0.0.0" }));
    copyFileSync(process.execPath, join(pkg, "bin", windows ? "langfuse-mcp.exe" : "langfuse-mcp"));
  }
  const script = join(root, "fake-binary.cjs");
  writeFileSync(script, fakeBinaryScript);
  return { root, shim, script };
}

// runShim runs the shim to completion with stdin and extra environment entries.
function runShim(shim, args, env = {}, input = "", nodeArgs = []) {
  return spawnSync(process.execPath, [...nodeArgs, shim, ...args], {
    input, env: { ...process.env, ...env }, encoding: "utf8", timeout: 30_000,
  });
}

test("passes arguments, environment and stdin to the binary byte-for-byte, never through a shell", (t) => {
  const { shim, script } = install(t);

  const got = runShim(shim, [script, hostile, "--flag=" + hostile], { SMOKE_VALUE: hostile }, "stdin line\n");

  assert.equal(got.status, 0, got.stderr);
  assert.deepEqual(JSON.parse(got.stdout), { argv: [hostile, "--flag=" + hostile], value: hostile, stdin: "stdin line\n" });
});

test("exits with the binary's exit code", (t) => {
  const { shim, script } = install(t);

  const got = runShim(shim, [script], { FAKE_EXIT: "7" });

  assert.equal(got.status, 7, got.stderr);
});

// startWaiting starts the shim over a waiting fake binary and resolves once the binary is ready,
// with the shim process, the binary's pid and a promise of the shim's exit. Both processes are
// killed when the test ends, so a failing test never leaves them running.
function startWaiting(t, shim, script, env) {
  const child = spawn(process.execPath, [shim, script], { env: { ...process.env, FAKE_MODE: "wait", ...env }, stdio: ["ignore", "pipe", "inherit"] });
  const exited = new Promise((resolve) => child.on("exit", (code, signal) => resolve({ code, signal })));
  let pid;
  t.after(() => {
    child.kill("SIGKILL");
    if (pid && alive(pid)) process.kill(pid, "SIGKILL");
  });
  return new Promise((resolve, reject) => {
    let out = "";
    child.stdout.on("data", (d) => {
      out += d;
      const m = /^ready (\d+)\n/.exec(out);
      if (m) {
        pid = Number(m[1]);
        resolve({ child, pid, exited });
      }
    });
    child.on("error", reject);
    exited.then(() => reject(new Error(`shim exited before the binary was ready: ${out}`)));
  });
}

// alive reports whether a process with this pid still exists.
function alive(pid) {
  try {
    process.kill(pid, 0);
    return true;
  } catch {
    return false;
  }
}

test("forwards SIGTERM to the binary and exits with the binary's exit code", { skip: windows && "no POSIX signals on Windows", timeout: 30_000 }, async (t) => {
  const { shim, script } = install(t);
  const { child, exited } = await startWaiting(t, shim, script, { FAKE_TRAP: "1" });

  child.kill("SIGTERM");

  assert.deepEqual(await exited, { code: 3, signal: null });
});

test("re-raises the signal that ended the binary, leaving no binary behind", { skip: windows && "no POSIX signals on Windows", timeout: 30_000 }, async (t) => {
  const { shim, script } = install(t);
  const { child, pid, exited } = await startWaiting(t, shim, script, {});

  child.kill("SIGTERM");

  assert.deepEqual(await exited, { code: null, signal: "SIGTERM" });
  assert.equal(alive(pid), false, `binary ${pid} still running`);
});

test("on an unsupported platform, exits non-zero naming the platform and the supported ones", (t) => {
  const { root, shim } = install(t);
  const preload = join(root, "unsupported-platform.cjs");
  writeFileSync(preload, `Object.defineProperty(process, "platform", { value: "aix" });\nObject.defineProperty(process, "arch", { value: "ppc64" });\n`);

  const got = runShim(shim, [], {}, "", ["--require", preload]);

  assert.equal(got.status, 1);
  assert.match(got.stderr, /aix-ppc64/);
  for (const supported of ["darwin-arm64", "darwin-x64", "linux-arm64", "linux-x64", "win32-arm64", "win32-x64"]) {
    assert.match(got.stderr, new RegExp(supported));
  }
  assert.equal(got.stdout, "");
});

test("without the platform package, exits non-zero naming the missing package", (t) => {
  const { shim } = install(t, { withoutPlatformPackage: true });

  const got = runShim(shim, []);

  assert.equal(got.status, 1);
  assert.match(got.stderr, new RegExp(`langfuse-api-mcp-${process.platform}-${process.arch}`));
  assert.equal(got.stdout, "");
});
