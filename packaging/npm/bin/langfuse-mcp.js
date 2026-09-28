#!/usr/bin/env node
// langfuse-mcp: the npm package's only bin entry (spec #119). It runs the Go binary that npm
// installed for this platform as the optional dependency langfuse-api-mcp-<platform>-<arch>, with
// the arguments and the environment unchanged, stdio inherited and never through a shell, forwards
// termination signals to it and exits the way it exited. Nothing is downloaded, at install time or
// here: the package has no install script.
"use strict";

const { spawn } = require("node:child_process");
const { constants } = require("node:os");
const path = require("node:path");

// The platforms a binary package exists for, as Node names them (process.platform-process.arch).
const supported = ["darwin-arm64", "darwin-x64", "linux-arm64", "linux-x64", "win32-arm64", "win32-x64"];

const platform = `${process.platform}-${process.arch}`;
if (!supported.includes(platform)) {
  fail(`no binary for this platform (${platform}); supported: ${supported.join(", ")}. ` +
    "Use a release archive, the container image or go install instead.");
}

const pkg = `langfuse-api-mcp-${platform}`;
let binary;
try {
  binary = path.join(path.dirname(require.resolve(`${pkg}/package.json`)), "bin",
    process.platform === "win32" ? "langfuse-mcp.exe" : "langfuse-mcp");
} catch {
  fail(`the package ${pkg} is not installed: it holds the binary for this platform and is an ` +
    "optional dependency of langfuse-api-mcp; reinstall without --omit=optional or --no-optional.");
}

const child = spawn(binary, process.argv.slice(2), { stdio: "inherit", shell: false, windowsHide: true });

// A signal to the shim goes to the binary; the shim then ends when the binary does.
const forwarded = ["SIGINT", "SIGTERM", "SIGHUP"];
for (const signal of forwarded) {
  process.on(signal, () => child.kill(signal));
}

child.on("error", (err) => fail(`cannot start ${binary}: ${err.message}`));
child.on("exit", (code, signal) => {
  if (signal) {
    // Die of the same signal, so the host sees what it would see without the shim.
    // A signal Node ignores (SIGPIPE) leaves the shim alive: it then exits 128 + the signal
    // number, as a shell reports a death by signal.
    for (const s of forwarded) process.removeAllListeners(s);
    process.exitCode = 128 + (constants.signals[signal] ?? 0);
    process.kill(process.pid, signal);
    return;
  }
  process.exit(code);
});

function fail(message) {
  process.stderr.write(`langfuse-mcp: ${message}\n`);
  process.exit(1);
}
