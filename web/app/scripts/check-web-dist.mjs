#!/usr/bin/env node
import { existsSync, readFileSync } from "node:fs";
import { join } from "node:path";
import { spawnSync } from "node:child_process";

const root = process.cwd();
const distRel = "go/internal/controlapi/web_dist";
const distDir = join(root, distRel);
const tracked = process.argv.includes("--tracked");
const missing = [];

function normalizeRef(ref) {
  if (!ref.startsWith("/")) return null;
  return ref.slice(1).split(/[?#]/, 1)[0];
}

function assertExists(rel) {
  const path = join(distDir, rel);
  if (!existsSync(path)) {
    missing.push(`${rel} (missing from ${distRel})`);
    return;
  }
  if (tracked) {
    const git = spawnSync("git", ["ls-files", "--error-unmatch", `${distRel}/${rel}`], {
      cwd: root,
      stdio: "ignore",
    });
    if (git.status !== 0) {
      missing.push(`${rel} (not tracked by git)`);
    }
  }
}

const indexPath = join(distDir, "index.html");
if (!existsSync(indexPath)) {
  missing.push("index.html (missing from go/internal/controlapi/web_dist)");
} else {
  const index = readFileSync(indexPath, "utf8");
  const attrPattern = /\b(?:src|href)="([^"]+)"/g;
  for (const match of index.matchAll(attrPattern)) {
    const rel = normalizeRef(match[1]);
    if (rel) assertExists(rel);
  }
}

const manifestPath = join(distDir, "manifest.webmanifest");
if (existsSync(manifestPath)) {
  const manifest = JSON.parse(readFileSync(manifestPath, "utf8"));
  for (const icon of manifest.icons ?? []) {
    if (typeof icon.src !== "string") continue;
    const rel = normalizeRef(icon.src.startsWith("/") ? icon.src : `/${icon.src}`);
    if (rel) assertExists(rel);
  }
}

if (missing.length > 0) {
  console.error("Embedded Web UI bundle is incomplete:");
  for (const item of missing) console.error(`  - ${item}`);
  process.exit(1);
}
