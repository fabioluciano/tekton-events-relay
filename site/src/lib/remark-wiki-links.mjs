import { visit } from 'unist-util-visit';

/**
 * Rewrites GitHub-wiki style links into routes served by this site.
 *
 * GitHub wiki pages link to each other by page name (the file name without the
 * `.md` extension), optionally with an anchor: `[text](Configuration-Reference#accumulator)`.
 * This plugin turns those into `<base>/docs/Configuration-Reference#accumulator`.
 *
 * External links (http/https/mailto), in-page anchors (`#foo`) and already
 * absolute paths (`/foo`) are left untouched.
 *
 * @param {{ base?: string }} [options]
 */
export function remarkWikiLinks(options = {}) {
  const base = (options.base || '').replace(/\/$/, '');

  return (tree) => {
    visit(tree, 'link', (node) => {
      const url = node.url || '';

      // Leave external links, mail links, anchors and absolute paths alone.
      if (/^([a-z][a-z0-9+.-]*:|\/\/|\/|#|\.)/i.test(url)) return;

      // Split off an optional anchor.
      const hashIndex = url.indexOf('#');
      const page = hashIndex === -1 ? url : url.slice(0, hashIndex);
      const anchor = hashIndex === -1 ? '' : url.slice(hashIndex);

      // Drop a trailing `.md` if a page happens to include it.
      const slug = page.replace(/\.md$/i, '');
      if (!slug) {
        // Pure anchor that slipped through – keep as-is.
        node.url = anchor;
        return;
      }

      node.url = `${base}/docs/${slug}${anchor}`;
    });
  };
}
