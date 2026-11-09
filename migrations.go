package agenda

import (
	"context"

	"github.com/arandu-io/framework/foundation"
	"github.com/arandu-io/hesape/database/migrations"
	"github.com/arandu-io/hesape/database/schema"
)

// Migrations declares the schema this module owns.
//
// They are returned in the order their names sort in, which is the order they
// apply in: the name carries the order, and nothing else decides it.
func (m *Module) Migrations() []foundation.Migration {
	return []foundation.Migration{createAgenda{}}
}

// The migration is reversible, and the assertion is here rather than discovered
// at rollback: the migrator tests for Down with a type assertion, so a Down
// with the wrong signature would leave a rollback that silently does nothing.
var _ migrations.ReversibleMigration = createAgenda{}

// createAgenda is the five tables this module owns, and the indexes its
// listings and its availability read by.
type createAgenda struct{ migrations.BaseMigration }

// GetName is the migration's identity, and it carries the order. It is fixed
// once the package is published: changing what an applied name means leaves the
// change missing everywhere it already ran, and nothing says so.
func (createAgenda) GetName() string { return "20261109_0001_create_agenda" }

// Up creates the tables and the indexes.
//
// The Blueprint spells each column for the engine the migration is running on,
// which is what lets one application develop on a file and deploy on Postgres
// without a second schema. The timestamps have no database default: the value
// comes from Go, which is also what lets a test hold the clock.
func (createAgenda) Up(ctx context.Context, conn migrations.Connection) error {
	if err := conn.Schema().Create(ctx, countersTable, func(table *schema.Blueprint) {
		// One row, "booking": the protocol sequence of every space, and the lock
		// two people booking the last place queue on.
		table.String("id").Primary()
		table.BigInteger("last_number").Default(0)
		table.Timestamp("updated_at")
	}); err != nil {
		return err
	}

	if err := conn.Schema().Create(ctx, servicesTable, func(table *schema.Blueprint) {
		table.String("id").Primary()
		table.String("key", maxKeyLen)
		table.String("name", maxNameLen)
		table.String("description", maxDescriptionLn).Default("")
		table.Integer("duration_minutes").Default(40)
		table.Integer("requires_payment").Default(0)
		table.Integer("active").Default(1)
		table.Timestamp("created_at")
		table.Timestamp("updated_at")

		table.Unique([]string{"key"}, "agenda_services_key_unique")
	}); err != nil {
		return err
	}

	if err := conn.Schema().Create(ctx, hoursTable, func(table *schema.Blueprint) {
		table.String("id").Primary()
		table.Integer("weekday")
		table.Integer("start_minute")
		table.Integer("end_minute")
		table.Timestamp("created_at")

		table.Index([]string{"weekday", "start_minute"}, "agenda_hours_weekday_idx")
	}); err != nil {
		return err
	}

	if err := conn.Schema().Create(ctx, closuresTable, func(table *schema.Blueprint) {
		table.String("id").Primary()
		table.Timestamp("starts_at")
		table.Timestamp("ends_at")
		table.String("reason", maxReasonLen).Default("")
		table.Timestamp("created_at")

		table.Index([]string{"starts_at", "ends_at"}, "agenda_closures_range_idx")
	}); err != nil {
		return err
	}

	return conn.Schema().Create(ctx, bookingsTable, func(table *schema.Blueprint) {
		table.String("id").Primary()
		table.String("tenant_id")
		table.String("code", maxCodeLen)
		table.String("service_key", maxKeyLen)
		table.String("service_name", maxNameLen).Default("")
		table.String("subject_type", maxIdentifierLen).Default("")
		table.String("subject_id", maxIdentifierLen).Default("")
		table.String("subject_label", maxLabelLen).Default("")
		table.String("requester_id", maxIdentifierLen)
		table.String("requester_name", maxNameLen).Default("")
		table.String("customer_note", maxNoteLen).Default("")
		table.Timestamp("starts_at")
		table.Timestamp("ends_at")
		table.String("status", 32)
		table.Timestamp("hold_until").Nullable()
		table.String("payment_ref", maxRefLen).Default("")
		table.String("released_by", maxIdentifierLen).Default("")
		table.String("released_reason", maxReasonLen).Default("")
		table.Timestamp("confirmed_at").Nullable()
		table.Timestamp("reminded_at").Nullable()
		table.Timestamp("completed_at").Nullable()
		table.String("completed_by", maxIdentifierLen).Default("")
		table.String("team_note", maxNoteLen).Default("")
		table.Integer("reschedules").Default(0)
		table.String("cancel_reason", maxReasonLen).Default("")
		table.String("cancelled_by", 16).Default("")
		table.Timestamp("created_at")
		table.Timestamp("updated_at")

		// The protocol names one booking, whatever the space.
		table.Unique([]string{"code"}, "agenda_bookings_code_unique")
		// The indexes match the ORDER BY and the WHERE of what reads the table:
		// the customer's list (tenant first), the availability of a day, the
		// team's list by day, and the two tasks.
		table.Index([]string{"tenant_id", "starts_at", "id"}, "agenda_bookings_tenant_starts_idx")
		table.Index([]string{"tenant_id", "requester_id", "starts_at"}, "agenda_bookings_tenant_requester_idx")
		table.Index([]string{"status", "starts_at"}, "agenda_bookings_status_starts_idx")
		table.Index([]string{"status", "hold_until"}, "agenda_bookings_status_hold_idx")
	})
}

// Down drops the tables, which takes their indexes with them.
func (createAgenda) Down(ctx context.Context, conn migrations.Connection) error {
	for _, table := range []string{bookingsTable, closuresTable, hoursTable, servicesTable, countersTable} {
		if err := conn.Schema().DropIfExists(ctx, table); err != nil {
			return err
		}
	}
	return nil
}
