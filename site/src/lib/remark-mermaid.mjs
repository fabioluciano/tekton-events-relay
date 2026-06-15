import { visit } from 'unist-util-visit';

const escapeHtml = (s) =>
  s
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;');

/**
 * Turns ```mermaid fenced code blocks into `<pre class="mermaid">` elements so
 * they are *not* syntax-highlighted by Shiki and can be rendered client-side by
 * mermaid.js. The diagram source is kept verbatim inside the element; the
 * client script (see DocsLayout) calls mermaid.run() over `.mermaid` nodes.
 */
export function remarkMermaid() {
  return (tree) => {
    visit(tree, 'code', (node) => {
      if (node.lang !== 'mermaid') return;
      const value = escapeHtml(node.value || '');
      node.type = 'html';
      node.value = `<pre class="mermaid" data-diagram>${value}</pre>`;
      delete node.lang;
      delete node.meta;
    });
  };
}
