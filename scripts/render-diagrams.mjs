import { execFileSync } from "node:child_process";
import { readdir, readFile, writeFile } from "node:fs/promises";
import path from "node:path";
import { pathToFileURL } from "node:url";

const root = path.resolve(import.meta.dirname, "..");
const diagramDir = path.join(root, "docs", "diagrams");

// Pele paints with CSS variables. Viewers that ignore those paint every
// fill black, so the build writes Pele's default colors as plain attributes.
const palette = {
  _bg: "#ffffff",
  _fg: "#1f1f1f",
  _m: "#6e6e6e",
  _l: "#1f1f1f",
  _s: "#f3f3f3",
  _a: "#e6e6e6",
  _b: "#8a8a8a",
  _c: "#5b7bd5",
  _r: "4px",
  _fm: "monospace",
  "pele-bg": "#ffffff",
  "pele-fg": "#1f1f1f",
  "pele-muted": "#6e6e6e",
  "pele-line": "#1f1f1f",
  "pele-surface": "#f3f3f3",
  "pele-surface-alt": "#e6e6e6",
  "pele-border": "#8a8a8a",
  "pele-accent": "#5b7bd5",
  "pele-radius": "4px",
  "pele-font": "sans-serif",
  "pele-font-mono": "monospace",
};

async function peleEntry() {
  const install = execFileSync("mise", ["where", "npm:pele"], {
    encoding: "utf8",
  }).trim();
  const found = await findPackage(install);
  if (!found) throw new Error(`pele package not found under ${install}`);
  return found;
}

async function findPackage(dir) {
  const entries = await readdir(dir, { withFileTypes: true });
  for (const entry of entries) {
    if (!entry.isDirectory() || entry.name === ".bin") continue;
    const found = await findPackage(path.join(dir, entry.name));
    if (found) return found;
  }
  const pkgPath = path.join(dir, "package.json");
  if (!entries.some((entry) => entry.name === "package.json")) return null;
  const pkg = JSON.parse(await readFile(pkgPath, "utf8"));
  if (pkg.name !== "pele") return null;
  return path.join(dir, "dist", "pele.js");
}

const { render } = await import(pathToFileURL(await peleEntry()));

const sources = (await readdir(diagramDir)).filter((name) => name.endsWith(".mmd")).sort();
if (sources.length === 0) {
  throw new Error(`no diagrams in ${diagramDir}`);
}

let failed = false;
for (const name of sources) {
  const stem = path.basename(name, ".mmd");
  const source = await readFile(path.join(diagramDir, name), "utf8");
  let svg;
  try {
    ({ svg } = render(source, {
      autoDirection: false,
      fontFamily: "sans-serif",
      idPrefix: stem,
      responsive: true,
    }));
  } catch (error) {
    failed = true;
    console.error(`${name}: ${error.message}`);
    continue;
  }
  svg = bakeColors(svg);
  const out = path.join(diagramDir, `${stem}.svg`);
  await writeFile(out, `${svg}\n`);
  console.log(path.relative(root, out));
}

if (failed) {
  process.exitCode = 1;
}

function bakeColors(svg) {
  let out = svg.replace(
    /var\(\s*--([A-Za-z0-9_-]+)\s*,\s*([^)]*)\)/g,
    (_, name, fallback) => palette[name] ?? fallback.trim(),
  );
  out = out.replace(/var\(\s*--([A-Za-z0-9_-]+)\s*\)/g, (_, name) => {
    if (!(name in palette)) {
      throw new Error(`unresolved CSS variable --${name}`);
    }
    return palette[name];
  });
  if (out.includes("var(")) {
    throw new Error("CSS variables remain in the SVG");
  }
  out = out.replace(/--[A-Za-z0-9_-]+:[^;"]*;?/g, "");
  out = out.replace(/style="rx:([^"]+)"/g, (_, radius) => {
    return `rx="${radius.trim().replace(/px$/, "")}"`;
  });
  out = out.replace(/style="([^"]*)"/g, (_, body) => {
    const cleaned = body.replace(/;{2,}/g, ";").replace(/^;+|;+$/g, "");
    return cleaned ? `style="${cleaned}"` : "";
  });
  const box = /viewBox="0 0 ([0-9.]+) ([0-9.]+)"/.exec(out);
  if (!box) throw new Error("SVG has no viewBox");
  return out.replace(
    /(<svg\b[^>]*>)/,
    `$1<rect width="${box[1]}" height="${box[2]}" fill="#ffffff"/>`,
  );
}
