import { beforeEach, describe, expect, it } from 'vitest';
import { useApp } from './app';

beforeEach(() => useApp.setState({ toasts: [], authQueue: [] }));

describe('useApp', () => {
  it('does not toast Cancelled errors', () => {
    useApp.getState().toast('{"kind":"Cancelled","message":"","stderr":""}');
    expect(useApp.getState().toasts).toHaveLength(0);
    useApp.getState().toast('{"kind":"Timeout","message":"","stderr":""}', 't1');
    expect(useApp.getState().toasts).toHaveLength(1);
  });
  it('removes a cancelled auth prompt by id', () => {
    const { pushAuth, cancelAuth } = useApp.getState();
    pushAuth({ id: 'a', prompt: 'Password:', secret: true });
    pushAuth({ id: 'b', prompt: 'Username:', secret: false });
    cancelAuth('a');
    expect(useApp.getState().authQueue.map((p) => p.id)).toEqual(['b']);
  });
});
