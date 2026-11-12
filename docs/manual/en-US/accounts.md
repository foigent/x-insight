# Accounts and project reading

This chapter describes the FPV24/schema47 source candidate. A local pass does not establish an installed release or Target acceptance.

## First owner and existing installations

The native installer initializes the database account authority once. On upgrade, a valid existing v1/v2/v3 local-account file is imported atomically: stable subjects, usernames, roles, disabled flags, and password hashes are retained. Preserve that private original for historical recovery. After initialization the running product uses the database exclusively; editing or restoring an old account file does not reset a password or change a live identity. Database unavailability fails closed.

A new installation without imported accounts has no default password. After installation and approved HTTPS configuration, an authorized host administrator uses the verified controller to create the first owner's private activation handoff. First create a private0700 directory at the chosen path. The output must be a new file in that directory.

```text
sudo /opt/x-insight/current/bin/xinsightctl access bootstrap-owner --username owner --output /approved/private/owner-activation.html --check
sudo /opt/x-insight/current/bin/xinsightctl access bootstrap-owner --username owner --output /approved/private/owner-activation.html
```

Check is read-only and creates no link or account. The real operation writes an owner-only0600 HTML handoff at the chosen path; its link points only to the installation's configured HTTPS origin. Deliver it securely to the holder. The holder opens the link, chooses and confirms a password of at least12 characters, then signs in normally. Links expire after15 minutes and can be used once. The browser removes the bearer from its URL and does not store it in browser storage. Do not paste the link into chat, issues, logs or shared documents. Keep the output private even after expiry. A handoff-file or commit failure requires inspection of the exact account and retained file before another attempt.

An existing administrator identity prevents creating another first owner. If normal owner access is unavailable, the authorized host administrator can inspect `access directory`, then use `access recover-account --subject EXACT_SUBJECT --expected-revision CURRENT_REVISION --output /approved/private/new-handoff.html --check` and the same exact command without `--check`. This is an explicit emergency path. It preserves the subject and grants, rejects disabled accounts, and does not require the administrator to know the holder's password. Do not edit database tables or the legacy account file.

## Normal account operations

Sign in as owner and open **Accounts** (`/access/accounts`). **Invite a new account** is for a username that does not exist. Review the invitation, then confirm the named target and action using your current password in that confirmation. To recover an existing reader, choose **Create recovery link** on that exact row under **Existing accounts**, review the named account, then confirm. This is separate from inviting a new account and from confirming a project-reading grant. Share the single-use activation/recovery link only with that holder; the holder sets their own password. Recovery immediately blocks the target's old password and sessions. Password completion and disable/enable advance only that account's authentication revision. Other people stay signed in; no service restart is required. Enabling a pending account still requires its activation. Normal account management cannot disable, demote or recover an owner. Usernames and stable subjects are not renamed here.

After the action succeeds, its result identifies the target and shows its activation or recovery link. **Awaiting password setup** means the link has been issued, not that the holder has completed it. The holder opens the complete link once, sets and confirms their own password, and sees which account was updated. Setting a password does not sign in as that account. If the browser still has another account signed in, use the explicit sign-out-and-sign-in action; the normal login form is prefilled with the verified target username. The signed-in page displays the actual server-verified account and role.

If a change succeeded but the list could not be refreshed, keep the result and refresh the state without repeating the change. If submission outcome is unknown, the page blocks another write until its state is checked. A password submission with an unknown outcome does not reuse the one-use link; check the current session or try normal sign-in. Do not infer failure merely from a missing confirmation or repeatedly generate links.

The normal entry follows current reading capabilities: a metadata-only account opens Runtime without probing economic summaries or resource activity it cannot read. A local denied panel does not log the account out. Expiry, revocation, or a changed account still closes protected data and requires a current session. Body-unavailable feedback does not establish that a body was captured or that this account has permission to read one.

## Owner reading is explicit

Management capability does not grant access to source data. In **Project reading**, select the intended account, registered source, exact project and allowed metadata fields. Preview the exact change, check the account/project/fields, then confirm with your current password. The page never copies another reader's grant. It cannot create wildcard, content, purpose, resource or budget grants, nor overwrite an advanced grant. Advanced scopes retain their existing native review path.

The same owner can now manage the product and read the explicitly granted project without switching accounts. An ungranted or different project remains denied. Details/coverage and selected context, identity, cost, runtime, attempt, capability, policy or destination metadata are independent of body reading. This page always leaves body reading disabled. Existing grants form a union: this editor cannot narrow another grant. Revoking its grant removes only that grant's access. A separate ordinary reader remains useful for checking least-privilege behavior.

## Backup and restore

Use the complete private native backup. Schema47 private bundles use the version2 manifest and bind account authority, credential/revision rows and activation-ticket rows to server-side digests; no credential values appear in a receipt. Recovery preserves those rows, stable subjects, existing grants and audit history. Before activation, the restored database retires all pre-restore activation/recovery links using the exact restore operation's receipt. A previously spent link cannot be revived from an older snapshot. Request a new recovery link when needed. Resume recognizes a committed retirement even if its journal-completion write was interrupted, while unrelated authority changes remain an integrity failure.

For a business-data backup that predates account authority, native recovery retains the current initialized database accounts in the empty forward-migrated target; it never takes current credentials from the obsolete host file. The business grants and facts remain those of the selected backup. A genuine pre-schema47 installation is initialized from its validated legacy file once. Review those different recovery states before execution. Source and destination databases are preserved until the existing approved retirement procedure permits removal.
