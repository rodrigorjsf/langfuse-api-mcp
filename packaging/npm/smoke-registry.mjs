// A read-only npm registry on 127.0.0.1 serving the tarballs `npm pack` wrote (spec #119, ticket
// #123), for the release workflow's npx smoke only: with npm pointed at it, `npx -y
// langfuse-api-mcp@<version>` resolves the main package and its optional platform packages the way
// it will from registry.npmjs.org, so the smoke proves that npm installs only this platform's
// package, with no install script, before the first `npm publish` (M7).
//
//   node packaging/npm/smoke-registry.mjs <tarball dir>
//
// It prints the registry URL on its first stdout line and serves until killed. Only the packuments
// (GET /<name>) and the tarballs (GET /<name>/-/<file>.tgz) exist; everything else is 404.
import { createHash } from "node:crypto";
import { readFileSync, readdirSync } from "node:fs";
import { createServer } from "node:http";
import { join, resolve } from "node:path";
import { gunzipSync } from "node:zlib";

if (process.argv.length !== 3) {
  console.error("usage: node packaging/npm/smoke-registry.mjs <tarball dir>");
  process.exit(2);
}
const dir = resolve(process.argv[2]);

// packageJSON returns package/package.json from an npm tarball (a gzipped ustar archive).
function packageJSON(tgz) {
  const tar = gunzipSync(tgz);
  for (let off = 0; off + 512 <= tar.length;) {
    const name = tar.toString("utf8", off, off + 100).replace(/\0.*$/s, "");
    if (name === "") break;
    const size = parseInt(tar.toString("utf8", off + 124, off + 136).replace(/\0.*$/s, "").trim() || "0", 8);
    if (name === "package/package.json") return JSON.parse(tar.toString("utf8", off + 512, off + 512 + size));
    off += 512 + Math.ceil(size / 512) * 512;
  }
  throw new Error("no package/package.json in the tarball");
}

const packages = new Map(); // name -> { manifest, file, bytes }: one version per package
for (const file of readdirSync(dir).filter((f) => f.endsWith(".tgz"))) {
  const bytes = readFileSync(join(dir, file));
  const manifest = packageJSON(bytes);
  packages.set(manifest.name, { manifest, file, bytes });
}

const server = createServer((req, res) => {
  if (req.method !== "GET") return send(res, 405, { error: "method not allowed" });
  let path;
  try {
    path = decodeURIComponent(new URL(req.url, "http://registry").pathname).replace(/^\/+/, "");
  } catch {
    return send(res, 400, { error: "bad request path" });
  }
  const origin = `http://${req.headers.host}`;
  const [pkgName, dash, tarballFile] = path.split("/");
  const tarball = packages.get(pkgName);
  if (dash === "-" && tarball?.file === tarballFile) {
    res.writeHead(200, { "Content-Type": "application/octet-stream" });
    return res.end(tarball.bytes);
  }
  const entry = packages.get(path);
  if (!entry) return send(res, 404, { error: "not found" });
  const { manifest, file, bytes } = entry;
  const dist = {
    tarball: `${origin}/${manifest.name}/-/${file}`,
    shasum: createHash("sha1").update(bytes).digest("hex"),
    integrity: "sha512-" + createHash("sha512").update(bytes).digest("base64"),
  };
  send(res, 200, {
    name: manifest.name,
    "dist-tags": { latest: manifest.version },
    versions: { [manifest.version]: { ...manifest, _id: `${manifest.name}@${manifest.version}`, dist } },
  });
});

function send(res, status, body) {
  res.writeHead(status, { "Content-Type": "application/json" });
  res.end(JSON.stringify(body));
}

server.listen(0, "127.0.0.1", () => {
  console.log(`http://127.0.0.1:${server.address().port}/`);
});
