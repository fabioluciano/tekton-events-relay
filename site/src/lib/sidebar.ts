import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { resolve } from 'node:path';

export interface NavItem {
  title: string;
  /** Page slug, e.g. "SCM-GitHub". Empty for non-page entries. */
  slug: string;
  /** Optional in-page anchor, including the leading "#". */
  anchor: string;
}

export interface NavSection {
  /** Section heading (e.g. "Getting started"); empty for the leading Home link. */
  title: string;
  items: NavItem[];
}

const LINK_RE = /\[([^\]]+)\]\(([^)]+)\)/;

function parseLink(text: string): NavItem | null {
  const m = text.match(LINK_RE);
  if (!m) return null;
  const title = m[1].trim();
  const target = m[2].trim();
  // Only internal wiki links become nav entries.
  if (/^[a-z][a-z0-9+.-]*:\/\//i.test(target)) {
    return { title, slug: '', anchor: '' };
  }
  const hash = target.indexOf('#');
  const slug = (hash === -1 ? target : target.slice(0, hash)).replace(/\.md$/i, '');
  const anchor = hash === -1 ? '' : target.slice(hash);
  return { title, slug, anchor };
}

/**
 * Parses the wiki's _Sidebar.md into structured navigation. The file uses a
 * simple convention:
 *
 *   **[🏠 Home](Home)**        -> standalone link (its own section)
 *   **Getting started**        -> section heading
 *   - [Quickstart](Quickstart) -> item under the current section
 */
export function parseSidebar(): NavSection[] {
  // The wiki submodule sits next to the Astro project root. During the build
  // the working directory is the project root (site/), so ../wiki resolves
  // correctly; fall back to a path relative to this module just in case.
  const candidates = [
    resolve(process.cwd(), '../wiki/_Sidebar.md'),
    fileURLToPath(new URL('../../../wiki/_Sidebar.md', import.meta.url)),
  ];
  let raw: string | null = null;
  for (const candidate of candidates) {
    try {
      raw = readFileSync(candidate, 'utf-8');
      break;
    } catch {
      /* try next candidate */
    }
  }
  if (raw === null) return [];

  const sections: NavSection[] = [];
  let current: NavSection | null = null;

  for (const line of raw.split('\n')) {
    const trimmed = line.trim();
    if (!trimmed) continue;

    const listMatch = trimmed.match(/^[-*]\s+(.*)$/);
    if (listMatch) {
      const item = parseLink(listMatch[1]);
      if (item) {
        if (!current) {
          current = { title: '', items: [] };
          sections.push(current);
        }
        current.items.push(item);
      }
      continue;
    }

    // A bold line is either a standalone link or a section heading.
    const bold = trimmed.match(/^\*\*(.+)\*\*$/);
    if (bold) {
      const inner = bold[1].trim();
      const asLink = parseLink(inner);
      if (asLink && asLink.slug) {
        // Standalone link such as the Home entry.
        current = { title: '', items: [asLink] };
        sections.push(current);
        current = null; // following items start a new section
      } else {
        current = { title: inner, items: [] };
        sections.push(current);
      }
      continue;
    }
  }

  return sections.filter((s) => s.title || s.items.length);
}

/** Ordered, de-duplicated list of page slugs for prev/next navigation. */
export function orderedSlugs(sections: NavSection[]): string[] {
  const seen = new Set<string>();
  const out: string[] = [];
  for (const section of sections) {
    for (const item of section.items) {
      // Only count primary page links (no anchor) for prev/next.
      if (item.slug && !item.anchor && !seen.has(item.slug)) {
        seen.add(item.slug);
        out.push(item.slug);
      }
    }
  }
  return out;
}
