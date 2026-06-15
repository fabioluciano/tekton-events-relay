import type { APIRoute } from 'astro';
import { getCollection } from 'astro:content';

function titleOf(slug: string, body: string): string {
  const m = body.match(/^#\s+(.+)$/m);
  return m ? m[1].replace(/[`*_]/g, '').trim() : slug.replace(/-/g, ' ');
}

function toText(md: string): string {
  return md
    .replace(/```[\s\S]*?```/g, ' ')
    .replace(/`[^`]*`/g, ' ')
    .replace(/!\[[^\]]*\]\([^)]*\)/g, ' ')
    .replace(/\[([^\]]*)\]\([^)]*\)/g, '$1')
    .replace(/^[#>*\-|]+/gm, ' ')
    .replace(/[*_~]/g, ' ')
    .replace(/\s+/g, ' ')
    .trim();
}

// Build-time search index: every wiki page is rendered statically, and this
// endpoint emits a small JSON document the docs page filters client-side.
export const GET: APIRoute = async () => {
  const pages = await getCollection('wiki');
  const index = pages.map((p) => {
    const body = p.body ?? '';
    return {
      slug: p.id,
      title: titleOf(p.id, body),
      text: toText(body).slice(0, 2400),
    };
  });
  return new Response(JSON.stringify(index), {
    headers: { 'Content-Type': 'application/json' },
  });
};
