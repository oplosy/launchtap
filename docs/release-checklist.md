# Release Checklist

Steps to complete before shipping a release to users.

## Before release

- [ ] Full test suite green in CI.
- [ ] Migration status reviewed (forward-only, rollback plan noted if any).
- [ ] Environment variables documented; `.env.example` updated for new settings.
- [ ] Breaking API or data changes called out in release notes.

## Releasing

- [ ] Tag the release and let CI produce artifacts.
- [ ] Smoke-test the deployed build against the checklist above.

## After release

- [ ] Watch error rates and key funnels for one working day.
- [ ] File follow-up issues for anything deferred.

Keep this list short; a long checklist is a checklist nobody reads.
