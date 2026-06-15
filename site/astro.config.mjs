// @ts-check
import { defineConfig } from 'astro/config';
import rehypeSlug from 'rehype-slug';
import { remarkWikiLinks } from './src/lib/remark-wiki-links.mjs';
import { remarkMermaid } from './src/lib/remark-mermaid.mjs';

// GitHub Pages project site: https://fabioluciano.github.io/tekton-events-relay/
const SITE = 'https://fabioluciano.github.io';
const BASE = '/tekton-events-relay';

// https://astro.build/config
export default defineConfig({
  site: SITE,
  base: BASE,
  trailingSlash: 'ignore',
  markdown: {
    // Wiki content is rendered to static HTML at build time (true SSG).
    remarkPlugins: [
      remarkMermaid,
      // Rewrite GitHub-wiki links ([text](PageName#anchor)) to /<base>/docs/PageName#anchor
      [remarkWikiLinks, { base: BASE }],
    ],
    rehypePlugins: [rehypeSlug],
    // Code blocks are always dark in this design, so a single dark theme is enough.
    shikiConfig: {
      theme: 'github-dark',
      wrap: false,
    },
  },
});
