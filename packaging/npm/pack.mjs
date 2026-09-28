// Builds the npm packages from GoReleaser's output (spec #119, ticket #123) and packs them with
// `npm pack`; nothing is published (`npm publish` runs only on a `v*` tag, wired by #126).
//
//   node packaging/npm/pack.mjs <goreleaser dist dir> <output dir>
//
// Run from the repository root after `goreleaser release --config packaging/.goreleaser.yaml`. It
// writes one tarball per package into <output dir>:
//   langfuse-api-mcp-<version>.tgz                  the main package: the shim (bin/langfuse-mcp.js)
//                                                   as its only bin entry, langfuse-mcp, and one
//                                                   optional dependency per platform package
//   langfuse-api-mcp-<platform>-<arch>-<version>.tgz  one per GoReleaser target, restricted by
//                                                   os/cpu, holding only that target's binary
// Every version is GoReleaser's (dist/metadata.json). No package has a script of any kind, so an
// install runs nothing and downloads nothing beyond the packages themselves. GoReleaser's own npm
// publisher is not used: it is Pro-only and downloads the binary at install time.
import { execFileSync } from "node:child_process";
import { chmodSync, copyFileSync, mkdirSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

if (process.argv.length !== 4) {
  console.error("usage: node packaging/npm/pack.mjs <goreleaser dist dir> <output dir>");
  process.exit(2);
}
const [distDir, outDir] = process.argv.slice(2).map((p) => resolve(p));

const here = dirname(fileURLToPath(import.meta.url));
const repoRoot = resolve(here, "..", "..");
const name = "langfuse-api-mcp";
const buildID = "langfuse-mcp"; // the build id in packaging/.goreleaser.yaml
const common = {
  license: "Apache-2.0",
  repository: { type: "git", url: "git+https://github.com/rodrigorjsf/langfuse-api-mcp.git" },
};

// GoReleaser target -> Node's process.platform and process.arch.
const platforms = { linux: "linux", darwin: "darwin", windows: "win32" };
const archs = { amd64: "x64", arm64: "arm64" };

const { version } = JSON.parse(readFileSync(join(distDir, "metadata.json"), "utf8"));
// artifacts.json holds paths relative to the directory GoReleaser ran in: the repository root.
const binaries = JSON.parse(readFileSync(join(distDir, "artifacts.json"), "utf8"))
  .filter((a) => a.type === "Binary" && a.extra?.ID === buildID);
if (binaries.length !== Object.keys(platforms).length * Object.keys(archs).length) {
  throw new Error(`want one binary per target in ${distDir}/artifacts.json, got ${binaries.length}`);
}

const stage = join(outDir, "stage");
rmSync(stage, { recursive: true, force: true });
mkdirSync(stage, { recursive: true });

// The shim knows the supported platforms by itself (it must run with nothing else installed); its
// list must be exactly the packages built here, or a platform would get a package the shim refuses,
// or a shim looking for a package that does not exist.
const shimSupported = JSON.parse(/const supported = (\[[^\]]*\]);/.exec(readFileSync(join(here, "bin", "langfuse-mcp.js"), "utf8"))[1]);
const built = binaries.map((b) => `${platforms[b.goos]}-${archs[b.goarch]}`).sort();
if (JSON.stringify(built) !== JSON.stringify([...shimSupported].sort())) {
  throw new Error(`the shim supports ${shimSupported.join(", ")}, but the packages built are ${built.join(", ")}`);
}

const optionalDependencies = {};
const packageDirs = [];
for (const b of binaries) {
  const os = platforms[b.goos];
  const cpu = archs[b.goarch];
  if (!os || !cpu) throw new Error(`unexpected target ${b.goos}/${b.goarch}`);
  const pkgName = `${name}-${os}-${cpu}`;
  const dir = join(stage, pkgName);
  const binary = join(dir, "bin", os === "win32" ? "langfuse-mcp.exe" : "langfuse-mcp");
  mkdirSync(dirname(binary), { recursive: true });
  copyFileSync(resolve(repoRoot, b.path), binary);
  chmodSync(binary, 0o755);
  copyFileSync(join(repoRoot, "LICENSE"), join(dir, "LICENSE"));
  writeJSON(join(dir, "package.json"), {
    name: pkgName, version,
    description: `The langfuse-mcp binary for ${os}-${cpu}, installed by ${name}; not meant to be installed directly.`,
    ...common, os: [os], cpu: [cpu], files: ["bin/"],
  });
  optionalDependencies[pkgName] = version;
  packageDirs.push(dir);
}

const main = join(stage, name);
mkdirSync(join(main, "bin"), { recursive: true });
copyFileSync(join(here, "bin", "langfuse-mcp.js"), join(main, "bin", "langfuse-mcp.js"));
chmodSync(join(main, "bin", "langfuse-mcp.js"), 0o755);
copyFileSync(join(repoRoot, "LICENSE"), join(main, "LICENSE"));
copyFileSync(join(repoRoot, "README.md"), join(main, "README.md"));
writeJSON(join(main, "package.json"), {
  name, version,
  description: "MCP server for the Langfuse public API (Cloud and self-hosted), with corporate CA and proxy support.",
  ...common, bin: { "langfuse-mcp": "bin/langfuse-mcp.js" }, files: ["bin/"],
  optionalDependencies: Object.fromEntries(Object.entries(optionalDependencies).sort()),
});
packageDirs.push(main);

for (const dir of packageDirs) {
  // --ignore-scripts: packing runs nothing either (no package has a script anyway).
  execFileSync("npm", ["pack", "--ignore-scripts", "--loglevel=warn", "--pack-destination", outDir], { cwd: dir, stdio: ["ignore", "ignore", "inherit"] });
}
rmSync(stage, { recursive: true, force: true });
console.log(`packed ${packageDirs.length} packages, version ${version}, into ${outDir}`);

function writeJSON(path, value) {
  writeFileSync(path, JSON.stringify(value, null, 2) + "\n");
}
