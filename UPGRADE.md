# Upgrade Guide

## Unreleased

Nothing to change in the code. The delete buttons of the team's lists (services,
hours, closures) are drawn by Kyse's `DeleteRow`, which draws `ypy-delete` now,
so the Kyse release that carries YPY is the one these views are compiled
against: with an older Kyse the published-view compile test, which looks for
`ypy-delete`, fails. An application that already published the views keeps its
own and gets the new buttons when it publishes them again.

## v0.1.0

First release: nothing to upgrade from. A new installation reads the README, in
this order: `go get`, the wiring (`TeamTenant` and `Zone` are the two required
settings), the actions in the arandu-permission catalogue, `aru migrate`, the
`Notifier`, the two tasks to schedule every five minutes, `Settle` where the
application learns a payment settled, and `aru vendor:publish` with
`aru view:build` for the screens. The package requires Framework v0.67.0,
Hesape v0.88.0, Kyse v0.45.0 and arandu-activitylog v0.7.1, and Go 1.26.10.
