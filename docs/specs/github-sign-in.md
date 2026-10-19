# GitHub sign-in

## What it does
A person signs in or creates an account with GitHub instead of an email link.
A new user gets the GitHub username as the name of their person actor.
If that name is taken or not valid, notid asks for another name and sends the person through GitHub again.
In invite mode, the person enters the invite code before going to GitHub.
A person whose user has the same verified email address goes into that user after their first GitHub sign-in.

## Decisions
- The installation uses a GitHub OAuth app through auth-all's `github` provider, set by `GITHUB_CLIENT_ID` and `GITHUB_CLIENT_SECRET` — auth-all owns OAuth; notid only configures it.
- The GitHub buttons show only when both variables are set — an installation without a GitHub app keeps email sign-in only.
- The name comes from the GitHub login, read from auth-all's provider username (auth-all issue #15) — GitHub's display name is not a valid name; typing a name by hand is the fallback only.
- The sign-up tab collects the invite code (invite mode) and, after a refusal, a name, before the GitHub step, in a short-lived signed cookie — GitHub cannot carry them, and the user must not exist before they are checked.
- A before-create hook refuses a new user when sign-up is closed, the invite is not valid, the name is taken or not valid, or the sign-up cookie is absent — a user without a person actor never exists.
- An after-create hook creates the person actor and uses the invite; if it fails, notid deletes the user — the same rule as email sign-up.
- On the sign-in tab, a GitHub login with no user gets "No account uses this GitHub login. Create one first." — sign-in never creates a user.
- auth-all's `AllowVerifiedEmailAutoLink` is on — GitHub verifies addresses, and a magic-link sign-in verifies them on the notid side.
- auth-all errors on the OAuth callback come back to `/login` as a message, not as JSON — the callback is a browser navigation.

## Out
- Google, OIDC or other providers.
- A Settings page to link or unlink GitHub.
- Using the GitHub avatar as the person actor's avatar.
- Password sign-in.

## How I know it works
- With the two variables unset, `/login` shows no GitHub button.
- In open mode, "Continue with GitHub" from a new GitHub account lands signed in, and the profile URL is `/u/<github login>`.
- In invite mode, an unused code plus GitHub creates the user and marks the code used; a used code shows an error on `/login` and creates no user.
- A GitHub login whose name is taken returns to `/login` asking for a name; the second attempt creates the user with the typed name.
- "Sign in with GitHub" from a GitHub account with no user shows the "Create one first" message, and `auth_users` gains no row.
- GitHub sign-in with the address of `nathan` lands as `nathan`, an admin.
- In closed mode, only existing users get in with GitHub.
