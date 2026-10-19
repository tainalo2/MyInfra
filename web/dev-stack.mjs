import { spawn } from "node:child_process";
import path from "node:path";
import { fileURLToPath } from "node:url";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const windows = process.platform === "win32";
const command = windows ? "powershell" : path.join(root, "dev");
const args = windows ? ["-NoProfile", "-ExecutionPolicy", "Bypass", "-File", path.join(root, "dev.ps1")] : [];
const child = spawn(command, args, { cwd: root, stdio: "inherit" });

child.on("exit", (code, signal) => {
  if (signal) process.kill(process.pid, signal);
  process.exit(code ?? 1);
});
