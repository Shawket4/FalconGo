-- =====================================================================
-- Fleet compartment backfill — cars.json_compartments
--
-- Source of truth: "Quick Fuel Brochure Redesign EN.pdf", page 07,
-- section "06 · Fleet & Equipment" — the 20-unit fleet table.
-- All 20 compartment lists reconcile with their printed totals
-- (grand total 1,053,500 L).
--
-- RULE: production cars.tank_capacity is authoritative and is NEVER
-- written by this script. A car whose brochure compartment sum does not
-- equal its stored tank_capacity is SKIPPED and reported, never guessed.
--
-- Run the three steps IN ORDER in pgAdmin4. Step 1 writes nothing.
-- =====================================================================


-- =====================================================================
-- STEP 1 — PREFLIGHT (read-only). Run this first and send back both
-- result grids before running Step 2.
--
-- Cars are keyed on the DIGIT GROUP of the tractor plate. The 20 digit
-- groups are unique across the fleet, so they identify a row on their
-- own; the Arabic letters travel along only so you can eyeball the
-- match. If the plate format in the DB does not yield a digit group
-- (e.g. Arabic-Indic numerals ٠١٢٣), 1a will show it and the keying
-- gets adjusted before anything is written.
-- =====================================================================

-- 1a. What is actually in the table right now.
SELECT id,
       car_no_plate,
       tank_capacity,
       json_compartments,
       substring(car_no_plate from '[0-9]+') AS plate_digits
FROM cars
WHERE deleted_at IS NULL
ORDER BY id;


-- 1b. Brochure vs. database, row by row.
WITH brochure(seq, plate_digits, plate_letters, comps, total) AS (
    VALUES
        ( 1, '8567', 'ف ج م', '[27000,27000]'::jsonb,                   54000),
        ( 2, '5918', 'ف ج م', '[27000,27000]'::jsonb,                   54000),
        ( 3, '5917', 'ف ج م', '[27000,27000]'::jsonb,                   54000),
        ( 4, '4253', 'ف ق ص', '[17000,17000,17000]'::jsonb,             51000),
        ( 5, '4381', 'ف ع ص', '[15000,12000,12000,15000]'::jsonb,       54000),
        ( 6, '6584', 'ف د ى', '[16500,16500,16500]'::jsonb,             49500),
        ( 7, '5937', 'ف ل ص', '[32000,32000]'::jsonb,                   64000),
        ( 8, '9156', 'ف ى ر', '[13500,13500,13500,13500]'::jsonb,       54000),
        ( 9, '4593', 'ف س ر', '[18000,18000,18000]'::jsonb,             54000),
        (10, '8427', 'ف ى ر', '[21000,33000]'::jsonb,                   54000),
        (11, '6834', 'ف د ى', '[27000,27000]'::jsonb,                   54000),
        (12, '7421', 'ف ا ن', '[17000,17000,17000]'::jsonb,             51000),
        (13, '5139', 'ف ا ن', '[17000,17000,17000]'::jsonb,             51000),
        (14, '9247', 'ف س م', '[13000,13000,13000,13000]'::jsonb,       52000),
        (15, '6738', 'ف د ى', '[16000,16500,16000]'::jsonb,             48500),
        (16, '6359', 'ف ر ص', '[27000,27000]'::jsonb,                   54000),
        (17, '7364', 'ف ل ص', '[28000,26000]'::jsonb,                   54000),
        (18, '8351', 'ف ل ص', '[27000,27000]'::jsonb,                   54000),
        (19, '4316', 'ف ص ل', '[20500,10000,14500]'::jsonb,             45000),
        (20, '4317', 'ف ص ل', '[15500,16000,16000]'::jsonb,             47500)
),
db AS (
    SELECT id, car_no_plate, tank_capacity, json_compartments,
           substring(car_no_plate from '[0-9]+') AS plate_digits
    FROM cars
    WHERE deleted_at IS NULL
)
SELECT
    CASE
        WHEN db.id IS NULL                        THEN '✗ NO CAR FOR BROCHURE ROW'
        WHEN b.seq IS NULL                        THEN '✗ CAR NOT IN BROCHURE'
        WHEN db.tank_capacity IS DISTINCT FROM b.total
                                                  THEN '✗ TOTAL MISMATCH — WILL SKIP'
        WHEN db.json_compartments IS NOT NULL
             AND db.json_compartments <> 'null'::jsonb
                                                  THEN '! ALREADY SET — WILL OVERWRITE'
        ELSE '✓ WILL UPDATE'
    END                              AS verdict,
    b.seq                            AS brochure_no,
    db.id                            AS car_id,
    db.car_no_plate,
    b.plate_letters                  AS brochure_letters,
    db.tank_capacity                 AS db_total,
    b.total                          AS brochure_total,
    db.json_compartments             AS current_compartments,
    b.comps                          AS new_compartments
FROM brochure b
FULL OUTER JOIN db ON db.plate_digits = b.plate_digits
ORDER BY verdict, b.seq NULLS LAST, db.id;

-- Expected: 20 rows, all '✓ WILL UPDATE'. Anything else — stop and
-- resolve it before Step 2. In particular '✗ CAR NOT IN BROCHURE' rows
-- are cars the brochure does not cover; they are left untouched.


-- =====================================================================
-- STEP 2 — UPDATE. Runs inside a transaction and prints what it did
-- before you commit. Do NOT commit unless the counts look right.
-- =====================================================================

BEGIN;

WITH brochure(plate_digits, comps, total) AS (
    VALUES
        ('8567', '[27000,27000]'::jsonb,             54000),
        ('5918', '[27000,27000]'::jsonb,             54000),
        ('5917', '[27000,27000]'::jsonb,             54000),
        ('4253', '[17000,17000,17000]'::jsonb,       51000),
        ('4381', '[15000,12000,12000,15000]'::jsonb, 54000),
        ('6584', '[16500,16500,16500]'::jsonb,       49500),
        ('5937', '[32000,32000]'::jsonb,             64000),
        ('9156', '[13500,13500,13500,13500]'::jsonb, 54000),
        ('4593', '[18000,18000,18000]'::jsonb,       54000),
        ('8427', '[21000,33000]'::jsonb,             54000),
        ('6834', '[27000,27000]'::jsonb,             54000),
        ('7421', '[17000,17000,17000]'::jsonb,       51000),
        ('5139', '[17000,17000,17000]'::jsonb,       51000),
        ('9247', '[13000,13000,13000,13000]'::jsonb, 52000),
        ('6738', '[16000,16500,16000]'::jsonb,       48500),
        ('6359', '[27000,27000]'::jsonb,             54000),
        ('7364', '[28000,26000]'::jsonb,             54000),
        ('8351', '[27000,27000]'::jsonb,             54000),
        ('4316', '[20500,10000,14500]'::jsonb,       45000),
        ('4317', '[15500,16000,16000]'::jsonb,       47500)
)
UPDATE cars c
SET    json_compartments = b.comps
FROM   brochure b
WHERE  c.deleted_at IS NULL
  AND  substring(c.car_no_plate from '[0-9]+') = b.plate_digits
  -- The safety rule: only touch a car whose stored capacity agrees
  -- with the brochure. tank_capacity itself is never written.
  AND  c.tank_capacity = b.total;

-- Expect: UPDATE 20. If it is fewer, Step 1b named the reason.


-- =====================================================================
-- STEP 3 — VERIFICATION. Run before COMMIT.
-- =====================================================================

-- 3a. Every brochure car now has compartments summing to its capacity.
--     ZERO ROWS = PASS.
SELECT id, car_no_plate, tank_capacity, json_compartments,
       (SELECT sum(e::numeric)
          FROM jsonb_array_elements_text(json_compartments) e) AS compartment_sum
FROM cars
WHERE deleted_at IS NULL
  AND substring(car_no_plate from '[0-9]+') IN (
        '8567','5918','5917','4253','4381','6584','5937','9156','4593','8427',
        '6834','7421','5139','9247','6738','6359','7364','8351','4316','4317')
  AND (
        json_compartments IS NULL
     OR json_compartments = 'null'::jsonb
     OR jsonb_typeof(json_compartments) <> 'array'
     OR jsonb_array_length(json_compartments) = 0
     OR EXISTS (SELECT 1 FROM jsonb_array_elements(json_compartments) e
                 WHERE jsonb_typeof(e) <> 'number' OR e::numeric <= 0)
     OR (SELECT sum(e::numeric)
           FROM jsonb_array_elements_text(json_compartments) e) <> tank_capacity
  );

-- 3b. Roll-up sanity: expect 20 cars and 1,053,500 L.
SELECT count(*)            AS cars_configured,
       sum(tank_capacity)  AS total_litres
FROM cars
WHERE deleted_at IS NULL
  AND json_compartments IS NOT NULL
  AND jsonb_typeof(json_compartments) = 'array'
  AND jsonb_array_length(json_compartments) > 0;


-- COMMIT;    -- uncomment and run once 3a returns zero rows
-- ROLLBACK;  -- otherwise
