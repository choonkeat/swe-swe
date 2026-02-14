#!/usr/bin/env node

import { spawnSync } from "child_process";
import { createRequire } from "module";
import { existsSync, chmodSync } from "fs";
import { dirname, join } from "path";
import { fileURLToPath } from "url";

const require = createRequire(import.meta.url);
const __dirname = dirname(fileURLToPath(import.meta.url));

const PLATFORM_MAP = {
  linux: "linux",
  darwin: "darwin",
  win32: "win32",
};

const ARCH_MAP = {
  x64: "x64",
  arm64: "arm64",
};

const platform = PLATFORM_MAP[process.platform];
const arch = ARCH_MAP[process.arch];

if (!platform || !arch) {
  console.error(
    `Unsupported platform: ${process.platform}-${process.arch}\n` +
      `swe-swe supports: linux-x64, linux-arm64, darwin-x64, darwin-arm64, win32-x64, win32-arm64`
  );
  process.exit(1);
}

const pkgName = `@choonkeat/swe-swe-${platform}-${arch}`;
const binName = process.platform === "win32" ? "swe-swe.exe" : "swe-swe";

let binPath;
try {
  const pkgDir = dirname(require.resolve(`${pkgName}/package.json`));
  binPath = join(pkgDir, "bin", binName);
} catch {
  // Fallback: check for local build in npm-platforms/ (development)
  const localPath = join(__dirname, "..", "npm-platforms", `${platform}-${arch}`, "bin", binName);
  if (existsSync(localPath)) {
    binPath = localPath;
  } else {
    console.error(
      `Could not find package ${pkgName}.\n` +
        `Make sure it is installed — this usually means your platform is supported\n` +
        `but the optional dependency was not installed.\n\n` +
        `Try: npm install ${pkgName}\n` +
        `Or run: npx swe-swe`
    );
    process.exit(1);
  }
}

if (!existsSync(binPath)) {
  console.error(`Binary not found at ${binPath}`);
  process.exit(1);
}

function run() {
  const result = spawnSync(binPath, process.argv.slice(2), {
    stdio: "inherit",
  });

  if (result.error) {
    return result;
  }
  process.exit(result.status ?? 1);
}

let result = run();

// Handle EACCES by chmod +x and retrying
if (result.error && result.error.code === "EACCES") {
  try {
    chmodSync(binPath, 0o755);
  } catch (e) {
    console.error(`Failed to chmod +x ${binPath}: ${e.message}`);
    process.exit(1);
  }
  result = run();
}

if (result.error) {
  console.error(`Failed to start swe-swe: ${result.error.message}`);
  process.exit(1);
}
