// Build a URL that respects the configured `base` path (GitHub Pages subpath).
const BASE = import.meta.env.BASE_URL.replace(/\/$/, '');

export function withBase(path: string): string {
  if (!path.startsWith('/')) path = '/' + path;
  return `${BASE}${path}`;
}

export const docsHref = (slug: string, anchor = ''): string =>
  withBase(`/docs/${slug}${anchor}`);

export const WIKI_URL = 'https://github.com/fabioluciano/tekton-events-relay/wiki';
export const REPO_URL = 'https://github.com/fabioluciano/tekton-events-relay';
