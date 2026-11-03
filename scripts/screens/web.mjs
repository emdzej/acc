// Captures the web dashboard screenshots from the demo daemon (see capture.sh).
//   node web.mjs <server-url> <token> <out-dir>
// PW is a directory whose node_modules has playwright.
import { createRequire } from "node:module";
const require = createRequire((process.env.PW || process.cwd()) + "/");
const { chromium } = require("playwright");
const [base, token, out] = process.argv.slice(2);
const ids = { api: "7116efd3-730d-4c1e-9a52-3f0d2e8b6a10", web: "9b2da2a2-c451-4e0b-8d7c-5a1f3c9e2b47",
  docs: "ses_19cb2a7e5ffeK2mDq8VzLwT3nP", codex: "019a2f4e-8b1c-7d3e-a5f6-4c2b9e8d7a10" };
const browser = await chromium.launch();
async function page(theme) {
  const ctx = await browser.newContext({ viewport: { width: 1440, height: 760 }, deviceScaleFactor: 2 });
  await ctx.addInitScript(t => localStorage.setItem("acc-theme", t), theme);
  return ctx.newPage();
}
async function shot(theme, hash, name, act) {
  const p = await page(theme);
  await p.goto(`${base}/?token=${token}#${hash}`);
  await p.waitForTimeout(1500);
  if (act) await act(p);
  await p.screenshot({ path: `${out}/${name}.png` });
  console.log(name);
  await p.context().close();
}
await shot("dark", `s=${ids.api}`, "web-dark");
await shot("dark", `s=${ids.web}`, "web-permission");
await shot("light", `s=${ids.docs}`, "web-opencode-light");
await shot("dark", `s=${ids.codex}`, "web-codex");
await shot("dark", "", "web-history", async p => {
  await p.click("#history"); await p.waitForTimeout(800);
  await p.keyboard.press("ArrowDown"); await p.keyboard.press("ArrowDown"); await p.waitForTimeout(400);
});
await shot("dark", "", "web-new-session", async p => {
  await p.click("#newsession"); await p.waitForTimeout(600);
  await p.selectOption("#launchagent select", "gemini").catch(() => {});
  await p.fill("#launch [name=cwd]", "/Users/me/code/pipeline").catch(() => {});
  await p.fill("#launch [name=title]", "schema-migration").catch(() => {});
  await p.waitForTimeout(400);
});
await browser.close();
