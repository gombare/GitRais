import { expect, test, type Page } from '@playwright/test';
import { execFileSync } from 'node:child_process';
import { writeFileSync } from 'node:fs';
import { join } from 'node:path';

// Set by scripts/e2e.sh: a throwaway repo with a local bare "origin" and the bundled git.
const env = (name: string) => {
  const v = process.env[name];
  if (!v) throw new Error(`${name} is not set: run these tests through scripts/e2e.sh`);
  return v;
};
const GIT = () => process.env.GITRAIS_E2E_GIT || 'git';
const run = (dir: string, ...args: string[]) =>
  execFileSync(GIT(), ['-C', dir, ...args], { encoding: 'utf8', stdio: ['ignore', 'pipe', 'pipe'] }).trim();
const repoGit = (...args: string[]) => run(env('GITRAIS_E2E_REPO'), ...args);
const bareGit = (...args: string[]) => run(env('GITRAIS_E2E_BARE'), ...args);
// Like bareGit, but "" while the command fails (e.g. the branch does not exist yet): for polling.
const bareGitOrEmpty = (...args: string[]) => {
  try { return bareGit(...args); } catch { return ''; }
};

// The scenarios share one repository and one page, so they depend on each other's result.
test.describe.configure({ mode: 'serial' });

let page: Page;

test.beforeAll(async ({ browser }) => {
  page = await (await browser.newContext({ viewport: { width: 1400, height: 900 } })).newPage();
});
test.afterAll(async () => {
  await page.context().close();
});

// No error/warning toast or hook error may show up (not role=alert: Monaco has its own empty
// aria alerts). Success and info notices are fine.
const noToasts = async () => {
  const alerts = page.locator('.toasts .toast-error, .toasts .toast-warning, .hook-error');
  await expect(alerts, `unexpected alerts: ${(await alerts.allTextContents()).join(' | ')}`).toHaveCount(0);
};
const firstRow = () => page.getByRole('option').first();

// Stages everything from the WIP panel and commits it with the given subject.
async function commitAll(subject: string) {
  await page.locator('.graph-wip').click();
  await page.getByRole('button', { name: 'Stage tutto', exact: true }).click();
  await page.getByRole('textbox', { name: 'Oggetto del commit' }).fill(subject);
  const commit = page.locator('[data-command="commit:commit"]');
  await expect(commit).toBeEnabled();
  await commit.click();
  await expect(firstRow()).toContainText(subject);
}

test('open the repository: the graph lists its commits', async () => {
  await page.goto('/');
  await page.waitForFunction(() => !!(window as any).go?.app?.App?.OpenRepo);
  await page.evaluate((p) => (window as any).go.app.App.OpenRepo(p), env('GITRAIS_E2E_REPO'));
  // RestoreTabs reuses the tab that is already open in the backend.
  await page.reload();
  await expect(page.getByRole('tab', { selected: true })).toHaveText('repo');
  await expect(page.getByRole('listbox', { name: 'Storia dei commit' })).toBeVisible();
  await expect(firstRow()).toContainText('initial');
  await noToasts();
});

test('edit a file: the watcher shows it in WIP; stage and commit', async () => {
  writeFileSync(join(env('GITRAIS_E2E_REPO'), 'e2e.txt'), 'hello\n');
  await expect(page.locator('.graph-wip')).toContainText('1 file modificati');
  await page.locator('.graph-wip').click();
  await expect(page.getByRole('button', { name: 'Apri diff e2e.txt' })).toBeVisible();
  await commitAll('e2e commit');
  await expect(page.locator('.graph-wip')).toHaveCount(0);
  expect(repoGit('log', '-1', '--format=%s')).toBe('e2e commit');
  expect(repoGit('status', '--porcelain')).toBe('');
  await noToasts();
});

test('push: the bare remote receives the commit', async () => {
  await page.getByRole('button', { name: 'Push', exact: true }).click();
  await expect.poll(() => bareGitOrEmpty('log', '-1', '--format=%s', 'main'), { timeout: 20_000 }).toBe('e2e commit');
  await expect(page.locator('.tb-op')).toHaveCount(0);
  await expect(firstRow()).toContainText('origin/main');
  expect(bareGit('rev-parse', 'main')).toBe(repoGit('rev-parse', 'HEAD'));
  await noToasts();
});

test('partial staging: a single hunk goes to the index, the other stays in the working tree', async () => {
  // multi.txt has 30 lines; changing lines 2 and 28 makes two separate hunks.
  const lines = Array.from({ length: 30 }, (_, i) => `line ${i + 1}`);
  lines[1] = 'line 2 changed';
  lines[27] = 'line 28 changed';
  writeFileSync(join(env('GITRAIS_E2E_REPO'), 'multi.txt'), lines.join('\n') + '\n');

  await expect(page.locator('.graph-wip')).toBeVisible();
  await page.locator('.graph-wip').click();
  await page.getByRole('button', { name: 'Apri diff multi.txt' }).click();
  // Monaco is a lazy chunk served by the dev server.
  await expect(page.locator('.diff-editor .monaco-diff-editor')).toBeVisible({ timeout: 30_000 });
  const blocks = page.getByRole('toolbar', { name: 'Blocchi di modifiche' }).getByRole('button');
  await expect(blocks).toHaveCount(2);
  await expect(blocks.first()).toBeEnabled();
  await blocks.first().click();

  await expect.poll(() => repoGit('diff', '--cached', '--', 'multi.txt'), { timeout: 10_000 }).toContain('+line 2 changed');
  const staged = repoGit('diff', '--cached', '--', 'multi.txt');
  expect(staged).not.toContain('line 28 changed');
  const unstaged = repoGit('diff', '--', 'multi.txt');
  expect(unstaged).toContain('+line 28 changed');
  expect(unstaged).not.toContain('line 2 changed');
  await noToasts();

  // Back to the graph and commit everything, leaving a clean tree.
  await page.getByRole('button', { name: 'Chiudi diff' }).click();
  await commitAll('e2e partial');
  expect(repoGit('status', '--porcelain')).toBe('');
});

test('create a branch from HEAD and check out another one', async () => {
  await page.getByRole('button', { name: 'Branch', exact: true }).click();
  const dialog = page.getByRole('dialog', { name: 'Nuovo branch da HEAD' });
  await dialog.getByRole('textbox', { name: 'Nome' }).fill('feature/e2e');
  await dialog.getByRole('button', { name: 'Crea branch' }).click();
  await expect.poll(() => repoGit('branch', '--show-current')).toBe('feature/e2e');
  await expect(page.locator('.left-panel .lp-name', { hasText: /^feature\/e2e$/ })).toBeVisible();

  // Double click on a local branch checks it out.
  await page.locator('.left-panel li', { has: page.locator('.lp-name', { hasText: /^main$/ }) }).dblclick();
  await expect.poll(() => repoGit('branch', '--show-current')).toBe('main');
  await expect(page.locator('.toolbar')).toContainText('main');
  await noToasts();
});
