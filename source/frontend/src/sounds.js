// Notification sounds. Built-in sounds ship with the app; "file:<path>" plays any
// .wav/.mp3/.ogg/.m4a on your PC (read through the Go side, cached after the first play).
import { Go } from "./api.js";
import chime from "./assets/sounds/chime.wav";
import ping from "./assets/sounds/ping.wav";
import pop from "./assets/sounds/pop.wav";
import bell from "./assets/sounds/bell.wav";
import blip from "./assets/sounds/blip.wav";

export const BUILTIN_SOUNDS = [
  ["chime", "Chime", chime], ["ping", "Ping", ping], ["pop", "Pop", pop], ["bell", "Bell", bell], ["blip", "Blip", blip],
];
const urls = Object.fromEntries(BUILTIN_SOUNDS.map(([id, , url]) => [id, url]));
const fileCache = {};
let lastPlay = 0;

export async function playSound(spec, volume = 70) {
  if (!spec) return;
  // don't machine-gun sounds when lots of pings arrive at once
  const now = Date.now();
  if (now - lastPlay < 400) return;
  lastPlay = now;
  let url = urls[spec];
  if (!url && spec.startsWith("file:")) {
    const path = spec.slice(5);
    url = fileCache[path] ??= await Go.GetSoundData(path);
  }
  if (!url) return;
  const a = new Audio(url);
  a.volume = Math.max(0, Math.min(1, (volume ?? 70) / 100));
  a.play().catch(() => {});
}

// for the Test buttons in Settings — always plays, ignoring the rate limit
export function previewSound(spec, volume) {
  lastPlay = 0;
  return playSound(spec, volume);
}
