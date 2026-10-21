// Renders docs/demo/index.html into docs/installer-demo.gif.
//   node docs/demo/render.mjs
//   PREVIEW=1.5,4,9 node docs/demo/render.mjs   writes PNGs of those moments to ./demo-preview instead
// Needs Chrome (google-chrome-stable, or set CHROME) and ffmpeg on PATH.

import { execFileSync, spawn } from "child_process";
import { mkdirSync, mkdtempSync, rmSync, writeFileSync } from "fs";
import { tmpdir } from "os";
import { dirname, join, resolve } from "path";
import { fileURLToPath } from "url";

const here = dirname(fileURLToPath(import.meta.url));
const out = resolve(here, "..", "installer-demo.gif");
const FPS = 25;
const WIDTH = 900, HEIGHT = 780;

const work = mkdtempSync(join(tmpdir(), "distra-demo-"));
const port = 9300 + Math.floor(Math.random() * 500);
const chrome = spawn(process.env.CHROME ?? "google-chrome-stable", [
    "--headless=new", "--disable-gpu", "--no-first-run", "--hide-scrollbars", "--force-color-profile=srgb",
    `--user-data-dir=${join(work, "profile")}`, `--remote-debugging-port=${port}`, "about:blank"
], { stdio: "ignore" });

try {
    let target;
    for (let i = 0; i < 100 && !target; i++) {
        await new Promise(r => setTimeout(r, 100));
        target = await fetch(`http://127.0.0.1:${port}/json`).then(r => r.json()).then(t => t.find(x => x.type === "page")).catch(() => null);
    }
    const ws = new WebSocket(target.webSocketDebuggerUrl);
    let id = 0;
    const pending = new Map();
    ws.onmessage = e => { const m = JSON.parse(e.data); pending.get(m.id)?.(m); };
    await new Promise(r => ws.onopen = r);
    const send = (method, params = {}) => new Promise(r => { pending.set(++id, r); ws.send(JSON.stringify({ id, method, params })); });
    const evaluate = async expression => (await send("Runtime.evaluate", { expression, awaitPromise: true, returnByValue: true })).result.result.value;

    await send("Emulation.setDeviceMetricsOverride", { width: WIDTH, height: HEIGHT, deviceScaleFactor: 1, mobile: false });
    await send("Page.navigate", { url: "file://" + join(here, "index.html") });
    while (!await evaluate("window.demoReady === true")) await new Promise(r => setTimeout(r, 100));

    if (process.env.PREVIEW) {
        const dir = resolve("demo-preview");
        mkdirSync(dir, { recursive: true });
        for (const t of process.env.PREVIEW.split(",")) {
            await evaluate(`renderAt(${Number(t)})`);
            const shot = await send("Page.captureScreenshot", { format: "png" });
            writeFileSync(join(dir, `t${t}.png`), Buffer.from(shot.result.data, "base64"));
        }
        console.log("wrote previews to", dir);
    } else {
        await renderGif(evaluate, send);
    }
    ws.close();
} finally {
    const exited = new Promise(r => chrome.once("exit", r));
    chrome.kill();
    await exited;
    rmSync(work, { recursive: true, force: true });
}

async function renderGif(evaluate, send) {
    const duration = await evaluate("window.DEMO_DURATION");
    const frames = Math.round(duration * FPS);
    for (let f = 0; f < frames; f++) {
        await evaluate(`renderAt(${f / FPS})`);
        const shot = await send("Page.captureScreenshot", { format: "png" });
        writeFileSync(join(work, `f${String(f).padStart(4, "0")}.png`), Buffer.from(shot.result.data, "base64"));
    }
    console.log(`rendered ${frames} frames (${duration.toFixed(2)}s)`);

    // A palette built from the whole clip, then error-diffusion dithering limited to changed areas, keeps flat UI
    // colors exact and the file small.
    const input = ["-framerate", String(FPS), "-i", join(work, "f%04d.png")];
    execFileSync("ffmpeg", ["-v", "error", "-y", ...input, "-vf", "palettegen=max_colors=256:stats_mode=full:reserve_transparent=0", join(work, "palette.png")]);
    execFileSync("ffmpeg", ["-v", "error", "-y", ...input, "-i", join(work, "palette.png"),
        "-lavfi", "paletteuse=dither=sierra2_4a:diff_mode=rectangle", "-loop", "0", out]);
    console.log("wrote", out);
}
