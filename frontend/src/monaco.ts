// Monaco setup, bundled locally (the app must work offline: no CDN loader).
// Only what a read-only diff viewer needs is imported: the editor API, the diff
// editor, a few contributions and the Monarch tokenizers (each loaded on
// demand). The language services (TypeScript, JSON, CSS, HTML) and their
// workers are left out on purpose. This module is only imported by the
// lazy-loaded DiffView chunk.
import * as monaco from 'monaco-editor/editor/editor.api';
import 'monaco-editor/features/codicon/register';
import 'monaco-editor/features/diffEditor/register';
import 'monaco-editor/features/find/register';
import 'monaco-editor/features/folding/register';
import 'monaco-editor/features/clipboard/register';
import 'monaco-editor/features/contextmenu/register';
import 'monaco-editor/languages/definitions/register.all';
import EditorWorker from 'monaco-editor/editor/editor.worker?worker';
import { loader } from '@monaco-editor/react';
import { gitraisTheme, MONACO_THEME } from './monacoTheme';

self.MonacoEnvironment = { getWorker: () => new EditorWorker() };
loader.config({ monaco });

// Colours come from the app's CSS tokens, read once when the diff chunk loads.
const css = getComputedStyle(document.documentElement);
monaco.editor.defineTheme(MONACO_THEME, gitraisTheme((name) => css.getPropertyValue(name)));

// Monaco language id for a file path (by extension or exact file name).
export function languageFor(path: string): string {
  const base = path.slice(path.lastIndexOf('/') + 1).toLowerCase();
  const dot = base.lastIndexOf('.');
  const ext = dot > 0 ? base.slice(dot) : '';
  const langs = monaco.languages.getLanguages();
  const hit = langs.find((l) => l.filenames?.some((f) => f.toLowerCase() === base))
    ?? (ext ? langs.find((l) => l.extensions?.includes(ext)) : undefined);
  return hit?.id ?? 'plaintext';
}
