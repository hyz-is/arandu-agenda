package agenda

import (
	"fmt"

	"github.com/arandu-io/framework/data"
	"github.com/arandu-io/framework/security"
)

// This file is the only place in the package that builds a Grant without a
// Policy answering for it, and the audit in tests/Unit holds that. Each of the
// two below is a Grant for work with no signed-in customer behind it, and each
// is issued after something else decided:
//
//   - taskGrant, for the scheduler that runs Remind and ExpireHolds. Nobody is
//     signed in; the Grant is the team's realm, which the configuration names,
//     and it reads which bookings are due across the spaces.
//   - scopedGrant, after TeamPolicy allowed the team member, or after taskGrant
//     for a task, for the one tenant the booking they act on lives in. The Grant
//     the team member holds is the realm's; a booking is in a customer's tenant,
//     and a statement needs the Grant of the tenant it reads. The scope is
//     checked against the action the realm Grant was issued for, so a Grant
//     issued to read cannot be scoped into one that writes something else.
//
// The application's own hooks, Settle and Find, take a Grant the application
// issued. appGrant checks it: it was issued for AgendaSettle, it names a tenant,
// and it can write.

// taskGrant is the Grant a task reads the due bookings with. The tenant is the
// team's realm, from Config.TeamTenant.
func taskGrant(realm string) (security.Grant, error) {
	if !security.ValidTenant(realm) {
		return security.Grant{}, fmt.Errorf("agenda: the team tenant %q cannot be one", realm)
	}
	//arandu:system-grant the scheduler has no session; the tenant is the team's realm, from the configuration, and each booking it changes is then scoped to its own tenant
	g := security.SystemGrant(AgendaRunTasks, realm)
	if err := g.Check(AgendaRunTasks); err != nil {
		return security.Grant{}, err
	}
	return g, nil
}

// scopedGrant scopes the realm Grant of an allowed team member, or of a task,
// to the tenant of one booking. The realm Grant must have been issued for the
// action asked.
func scopedGrant(realm security.Grant, a security.Action, tenant string) (security.Grant, error) {
	if err := realm.Check(a); err != nil {
		return security.Grant{}, err
	}
	if !security.ValidTenant(tenant) {
		return security.Grant{}, fmt.Errorf("agenda: a booking is in the tenant %q, which cannot be one", tenant)
	}
	//arandu:system-grant TeamPolicy allowed the team member in their realm, or the scheduler holds the realm Grant; each read or write is scoped to the one tenant the booking is in
	return security.SystemGrant(a, tenant), nil
}

// appGrant checks the Grant the application passed to one of its hooks. It
// decides nothing the application did not: the module trusts the Grant only as
// far as it was issued for AgendaSettle, for a tenant, and may write.
func appGrant(g security.Grant) (security.Grant, error) {
	if err := g.Check(AgendaSettle); err != nil {
		return security.Grant{}, err
	}
	if !security.ValidTenant(data.Tenant(g)) {
		return security.Grant{}, fmt.Errorf("agenda: the application's Grant names the tenant %q, which cannot be one", data.Tenant(g))
	}
	return g, nil
}
