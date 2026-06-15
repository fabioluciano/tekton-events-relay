import { defineCollection } from 'astro:content';
import { glob } from 'astro/loaders';

// The documentation lives in the project's GitHub wiki, vendored here as a git
// submodule at ../wiki. Each Markdown page becomes a statically rendered route
// under /docs/<PageName>. Files prefixed with "_" (e.g. _Sidebar.md, _Footer.md)
// are partials and are ignored by the glob loader automatically.
const wiki = defineCollection({
  loader: glob({
    // Exclude wiki partials (_Sidebar.md, _Footer.md) — they are not pages.
    pattern: ['*.md', '!_*.md'],
    base: '../wiki',
    // Keep the exact page name as the entry id (and therefore the URL slug),
    // matching how the wiki links between pages: [text](SCM-GitHub).
    generateId: ({ entry }) => entry.replace(/\.md$/i, ''),
  }),
});

export const collections = { wiki };
