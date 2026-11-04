import type { Embed } from '../../core/types';
/** Provider previews are optional; links remain usable without an external service. */
export async function readEmbedDetails(_embed: Embed): Promise<Embed | null> { return null; }
export async function publicPicture(url: string): Promise<string | null> {
  try { const parsed = new URL(url); return parsed.protocol === 'https:' ? parsed.href : null; } catch { return null; }
}
