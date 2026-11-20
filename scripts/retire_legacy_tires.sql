-- The old tire system (trucks, tires, tire_positions) is retired: tires are
-- recorded only in the maintenance system (maint-rust). The server no longer
-- reads, writes or migrates these three tables; they are left in place until
-- someone runs this by hand.
--
-- 1. Take a copy first (on the database host):
--      pg_dump "$DATABASE_URL" -t trucks -t tires -t tire_positions \
--          -Fc -f legacy_tires_$(date +%F).dump
--    and check it:  pg_restore -l legacy_tires_*.dump | grep -c 'TABLE DATA'   -- expect 3
-- 2. Then:
--      psql "$DATABASE_URL" -f scripts/retire_legacy_tires.sql
BEGIN;
DROP TABLE IF EXISTS tire_positions;
DROP TABLE IF EXISTS tires;
DROP TABLE IF EXISTS trucks;
COMMIT;
