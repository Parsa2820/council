// Browser check for the web UI (optional; needs Node 18+ and Playwright:
// `npm i playwright && npx playwright install chromium firefox webkit`).
//
// usage: node scripts/webui-check.mjs URL MODERATOR [SCREENSHOT.png]
//
// Against a running `council serve`, in Chromium, Firefox and WebKit:
// picks MODERATOR while the status checks are still running, changes the
// agent checkboxes, refreshes the status, then runs the council and checks
// that the request carried the moderator, that "checking…" badges turned into
// real ones, and that the result says "Method: moderator (MODERATOR)".
// Exits non-zero on the first failure.
import { chromium, firefox, webkit } from "playwright";

const [url, moderator, shot] = process.argv.slice(2);
if (!url || !moderator) {
  console.error("usage: node scripts/webui-check.mjs URL MODERATOR [SCREENSHOT.png]");
  process.exit(2);
}
const fail = (engine, msg) => { console.error(`FAIL ${engine}: ${msg}`); process.exit(1); };

for (const [engine, type] of [["chromium", chromium], ["firefox", firefox], ["webkit", webkit]]) {
  const browser = await type.launch();
  const page = await browser.newPage({ viewport: { width: 1360, height: 1000 } });
  const sent = [];
  page.on("request", (r) => { if (r.url().endsWith("/api/run")) sent.push(JSON.parse(r.postData()).moderator); });
  await page.goto(url);
  await page.waitForSelector("#agent-list input");
  // Pick the moderator before the (slow) status checks have finished.
  await page.selectOption("#moderator", moderator);
  if (!(await page.isVisible(".badge.checking"))) console.warn(`note ${engine}: status was already loaded when the moderator was picked`);
  await page.waitForSelector(".badge:not(.checking)", { timeout: 60000 });
  await page.waitForFunction(() => !document.querySelector(".badge.checking"), null, { timeout: 60000 });
  // Untick and tick agents (the moderator among them if it is listed), then refresh.
  for (const box of await page.$$("#agent-list input")) { await box.click(); await box.click(); }
  await page.click("#refresh");
  await page.waitForFunction(() => !document.querySelector(".badge.checking"), null, { timeout: 60000 });
  const value = await page.inputValue("#moderator");
  if (value !== moderator) fail(engine, `moderator reset to ${value}`);
  const badges = await page.$$eval(".badge", (bs) => bs.map((b) => b.textContent));
  if (badges.some((b) => b === "status unknown" || b === "checking…")) fail(engine, `badges ${badges}`);
  await page.fill("#prompt", "What is 6 * 7? Answer with just the number.");
  await page.click("#run");
  await page.waitForSelector("#final:not([hidden]), #error:not([hidden])", { timeout: 120000 });
  const meta = await page.textContent("#final-meta");
  if (sent[0] !== moderator) fail(engine, `request sent moderator ${sent[0]}`);
  if (!meta.startsWith(`Method: moderator (${moderator})`)) fail(engine, `result "${meta}"`);
  console.log(`ok ${engine}: moderator ${moderator} picked and used; badges ${badges.join(", ")}; ${meta}`);
  if (shot && engine === "chromium") {
    await page.evaluate(() => window.scrollTo(0, 0));
    await page.screenshot({ path: shot, fullPage: true });
  }
  await browser.close();
}
