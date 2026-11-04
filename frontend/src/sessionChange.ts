// Only a nonsecret notification is shared. Identity and permissions are always
// reread from the authenticated server response in each tab.
export const sessionChangeKey = "xinsight.session-change.v1";
export function announceSessionChange() {
  try { window.localStorage.setItem(sessionChangeKey, crypto.randomUUID()); }
  catch { /* Focus/pageshow and server-side enforcement remain available. */ }
}
