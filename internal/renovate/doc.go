// Package renovate is the worker's handler for a Renovate run of one repository
// (specs.html § 2.2 to § 2.5). It claims nothing itself: the worker runner hands it a leased
// job. The handler asks the app for the job's installation token over the internal listener,
// fetches or reads the global Renovate config, runs the pinned renovate binary as a second Unix
// user in a per-job scratch directory and a per-slot cache, and returns the attempt's record
// for the app to store. It opens no database and holds neither the App key nor the encryption
// key. The redactor and the global-config source live in their own packages so the app can
// reuse them.
package renovate
