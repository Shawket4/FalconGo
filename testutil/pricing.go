package testutil

import (
	"Falcon/Models"
	"testing"

	"gorm.io/gorm"
)

// TripSchema is PGSchema with the trip tables and the stored-pricing schema
// (migrated twice, as every boot re-runs it). Foreign keys gorm would infer
// from associations are skipped: the fixtures insert containers whose parent
// header is not always there, exactly as production has them.
func TripSchema(t *testing.T) *gorm.DB {
	t.Helper()
	db := PGSchema(t)
	db.Config.DisableForeignKeyConstraintWhenMigrating = true
	if err := db.AutoMigrate(
		&Models.User{}, &Models.Terminal{}, &Models.TerminalCompany{},
		&Models.ParentTrip{}, &Models.FeeMapping{}, &Models.TripStruct{}, &Models.ReceiptStep{},
	); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	for i := 0; i < 2; i++ {
		if err := Models.MigratePricing(db); err != nil {
			t.Fatalf("migrate pricing (run %d): %v", i+1, err)
		}
	}
	return db
}

// ApexRevenueFixture is apex-rust's tests/support/mod.rs seed_trip_fixture,
// verbatim but for column spelling: the rows its revenue parity suite and its
// golden snapshot (tests/fixtures/revenue_golden.txt) are built from.
const ApexRevenueFixture = `
        INSERT INTO fee_mappings (company, terminal, drop_off_point, distance, fee) VALUES
            ('Petrol Arrows', 'PA-T1', 'PA-Near',  120.0, 30.5),
            ('Petrol Arrows', 'PA-T1', 'PA-Far',   340.0, 44.25),
            ('Watanya',       'WA-T1', 'WA-B1',    100.0, 1),
            ('Watanya',       'WA-T1', 'WA-B2',    150.0, 2),
            ('Watanya',       'WA-T1', 'WA-B15',   900.0, 15),
            ('TAQA',          'TQ-T1', 'TQ-Site',  210.0, 0),
            ('Petromin',      'PM-T1', 'PM-Site',  180.0, 0);

        INSERT INTO trips (company, terminal, drop_off_point, car_no_plate, tank_capacity, date, receipt_no) VALUES
            ('Petrol Arrows','PA-T1','PA-Near',     'PA-A', 40000, '2025-05-05', 'PA1'),
            ('Petrol Arrows','PA-T1','PA-Near',     'PA-A', 36000, '2025-05-06', 'PA2'),
            ('Petrol Arrows','PA-T1','PA-Far',      'PA-B', 50000, '2025-05-07', 'PA3'),
            ('Petrol Arrows','PA-T1','PA-Unmapped', 'PA-B', 45000, '2025-05-08', 'PA4');

        INSERT INTO trips (company, terminal, drop_off_point, car_no_plate, tank_capacity, date, receipt_no) VALUES
            ('Watanya','WA-T1','WA-B1',       'WA-A', 40000, '2025-05-05', 'WA1'),
            ('Watanya','WA-T1','WA-B2',       'WA-A', 40000, '2025-05-06', 'WA2'),
            ('Watanya','WA-T1','WA-B15',      'WA-B', 30000, '2025-05-07', 'WA3'),
            ('Watanya','WA-T1','WA-Unmapped', 'WA-B', 30000, '2025-05-08', 'WA4');

        INSERT INTO trips (company, terminal, drop_off_point, car_no_plate, tank_capacity, date, receipt_no, parent_trip_id) VALUES
            ('Watanya','WA-T1','WA-B1','WA-C', 10000, '2025-05-09', 'WAC1', 9001),
            ('Watanya','WA-T1','WA-B1','WA-C', 10000, '2025-05-09', 'WAC2', 9001),
            ('Watanya','WA-T1','WA-B1','WA-C', 10000, '2025-05-09', 'WAC3', 9001);

        INSERT INTO trips (company, terminal, drop_off_point, car_no_plate, tank_capacity, date, receipt_no)
        SELECT 'TAQA','TQ-T1','TQ-Site','TQ-A', 30000,
               to_char(DATE '2025-05-01' + (n || ' days')::interval, 'YYYY-MM-DD'), 'TQA' || n
        FROM generate_series(0, 27) AS n;

        INSERT INTO trips (company, terminal, drop_off_point, car_no_plate, tank_capacity, date, receipt_no)
        SELECT 'TAQA','TQ-T1','TQ-Site','TQ-B', 25000,
               to_char(DATE '2025-05-01' + (n || ' days')::interval, 'YYYY-MM-DD'), 'TQB' || n
        FROM generate_series(0, 9) AS n;

        INSERT INTO trips (company, terminal, drop_off_point, car_no_plate, tank_capacity, date, receipt_no)
        SELECT 'TAQA','TQ-T1','TQ-Site','TQ-A', 30000,
               to_char(DATE '2025-06-01' + (n || ' days')::interval, 'YYYY-MM-DD'), 'TQAJ' || n
        FROM generate_series(0, 2) AS n;

        INSERT INTO trips (company, terminal, drop_off_point, car_no_plate, tank_capacity, date, receipt_no) VALUES
            ('Petromin','PM-T1','PM-Site','PM-A', 20000, '2025-05-18', 'PM1'),
            ('Petromin','PM-T1','PM-Site','PM-A', 20000, '2025-05-18', 'PM2'),
            ('Petromin','PM-T1','PM-Site','PM-A', 20000, '2025-05-19', 'PM3'),
            ('Petromin','PM-T1','PM-Site','PM-B', 20000, '2025-05-18', 'PM4');

        INSERT INTO parent_trips (id, car_id, driver_id, car_no_plate, driver_name,
                                  transporter, tank_capacity, company, terminal, date, author)
        VALUES (9001, 1, 1, 'WA-C', 'Container Driver', 'Apex', 30000,
                'Watanya', 'WA-T1', '2025-05-09', 'fixture');

        INSERT INTO trips (company, terminal, drop_off_point, car_no_plate, tank_capacity, date, receipt_no, deleted_at) VALUES
            ('Watanya','WA-T1','WA-B15','WA-Z', 99000, '2025-05-10', 'DEL1', now());
`
