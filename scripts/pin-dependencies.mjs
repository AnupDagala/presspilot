import { readFile, writeFile } from "node:fs/promises";
const lock = JSON.parse(await readFile("package-lock.json", "utf8"));
for (const path of [
  "package.json",
  "apps/web/package.json",
  "apps/worker/package.json",
]) {
  const file = JSON.parse(await readFile(path, "utf8"));
  for (const group of ["dependencies", "devDependencies"])
    for (const name of Object.keys(file[group] ?? {})) {
      const value = lock.packages["node_modules/" + name]?.version;
      if (value) file[group][name] = value;
    }
  await writeFile(path, JSON.stringify(file, null, 2) + "\n");
}
