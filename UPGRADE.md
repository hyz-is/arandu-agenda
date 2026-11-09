# Upgrade Guide

## Unreleased

## v0.1.0

First release: nothing to upgrade from. A new installation reads the README, in
this order: `go get`, the wiring (`TeamTenant` and `Zone` are the two required
settings), the actions in the arandu-permission catalogue, `aru migrate`, the
`Notifier`, the two tasks to schedule every five minutes, `Settle` where the
application learns a payment settled, and `aru vendor:publish` with
`aru view:build` for the screens. The package requires Framework v0.67.0,
Hesape v0.88.0, Kyse v0.45.0 and arandu-activitylog v0.7.1, and Go 1.26.10.
