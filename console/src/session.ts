import { reactive } from 'vue';
import { api, type Me, type SignInOptions } from './api';

// Who is signed in, shared by every view.
export const session = reactive<{ me: Me | null; options: SignInOptions | null; loaded: boolean; notice: string }>({
  me: null,
  options: null,
  loaded: false,
  notice: '', // shown once on the welcome page, e.g. after a session ends
});

export async function loadSession(): Promise<Me | null> {
  const s = await api.session();
  session.me = s.signedIn ? { role: s.role, login: s.login, expiresAt: s.expiresAt } : null;
  session.loaded = true;
  return session.me;
}

export async function signOut() {
  await api.logout();
  session.me = null;
}

// expire forgets a session the server no longer has. It reports whether there was one to forget.
export function expire(): boolean {
  if (!session.me) return false;
  session.notice = session.me.role === 'sandbox'
    ? 'Your sandbox has ended and its data is gone. You can start another.'
    : 'Your session has ended. Sign in again to continue.';
  session.me = null;
  return true;
}
