package services

import (
	"fmt"
	"testing"

	"Falcon/Models"
)

func TestLetterOf(t *testing.T) {
	cases := []struct {
		name string
		want string
	}{
		// The article is stripped before the key is taken. This is the rule the
		// whole scheme rests on.
		{"العياط", "ع"},
		{"الشيخ زايد 2", "ش"},
		{"البرلس للسفن", "ب"},
		{"الجبل الاحمر", "ج"},
		// A name that merely starts with ا keeps it.
		{"اسيوط الغربي1", "ا"},
		{"اكتوبر 2", "ا"},
		// ...including one where stripping the article still lands on ا.
		{"الاوسطي 1", "ا"},
		// Hamza carriers and ى/ة fold onto their base letter.
		{"أسيوط", "ا"},
		{"إمبابة", "ا"},
		{"آخر نقطة", "ا"},
		{"ىسر", "ي"},
		// Diacritics and tatweel carry no filing information.
		{"العَيَّاط", "ع"},
		{"العـــياط", "ع"},
		// Leading punctuation is noise.
		{"/الشيخ زايد", "ش"},
		{"  حلوان 1  ", "ح"},
		// Non-Arabic first characters get their own buckets.
		{"6 اكتوبر", BucketDigits},
		{"In garage", BucketLatin},
		{"", BucketUnset},
		{"—", BucketUnset},
		// "ال" alone has nothing left to file under, so it keeps its ا.
		{"ال", "ا"},
	}
	for _, c := range cases {
		if got := LetterOf(c.name); got != c.want {
			t.Errorf("LetterOf(%q) = %q, want %q", c.name, got, c.want)
		}
	}
}

// letterSeq builds a letter sequence with the given weights, for the
// partitioner. The letters themselves are irrelevant to the maths.
func letterSeq(weights ...int) []PileLetter {
	out := make([]PileLetter, 0, len(weights))
	for i, w := range weights {
		out = append(out, PileLetter{
			Letter:       string(ArabicAlphabet[i%len(ArabicAlphabet)]),
			ReceiptCount: w,
		})
	}
	return out
}

func loads(runs [][]PileLetter) []int {
	out := make([]int, 0, len(runs))
	for _, r := range runs {
		sum := 0
		for _, l := range r {
			sum += l.ReceiptCount
		}
		out = append(out, sum)
	}
	return out
}

// bruteForceOptimum is the definition the binary search must match: the lightest
// possible heaviest pile over every way of cutting the sequence into k
// consecutive runs.
func bruteForceOptimum(weights []int, k int) int {
	n := len(weights)
	best := -1
	var walk func(start, runsLeft, worst int)
	walk = func(start, runsLeft, worst int) {
		if runsLeft == 1 {
			sum := 0
			for _, w := range weights[start:] {
				sum += w
			}
			if sum > worst {
				worst = sum
			}
			if best < 0 || worst < best {
				best = worst
			}
			return
		}
		sum := 0
		for end := start; end <= n-runsLeft; end++ {
			sum += weights[end]
			w := worst
			if sum > w {
				w = sum
			}
			walk(end+1, runsLeft-1, w)
		}
	}
	walk(0, k, 0)
	return best
}

func TestPartitionKeepsLettersConsecutiveAndInOrder(t *testing.T) {
	letters := letterSeq(10, 3, 40, 5, 5, 20, 1, 1, 30)
	runs := partitionLetters(letters, 4)

	if len(runs) != 4 {
		t.Fatalf("got %d piles, want 4", len(runs))
	}
	// Flattening the piles must reproduce the input order exactly — that is
	// what makes a pile label like "ح خ د" findable.
	i := 0
	for _, run := range runs {
		if len(run) == 0 {
			t.Fatalf("empty pile in %v", loads(runs))
		}
		for _, l := range run {
			if l.Letter != letters[i].Letter {
				t.Fatalf("letter %d out of order: got %q want %q", i, l.Letter, letters[i].Letter)
			}
			i++
		}
	}
	if i != len(letters) {
		t.Fatalf("piles cover %d letters, want %d", i, len(letters))
	}
}

func TestPartitionIsOptimal(t *testing.T) {
	cases := [][]int{
		{10, 3, 40, 5, 5, 20, 1, 1, 30},
		{49, 1, 1, 49},
		{1, 1, 1, 1, 1, 1},
		{100, 1, 1, 1},
		{1, 1, 1, 100},
		{86, 27, 62, 78, 9, 37, 41, 50, 56, 56, 14}, // the reference workbook's boxes
	}
	for _, weights := range cases {
		for k := 1; k <= len(weights); k++ {
			runs := partitionLetters(letterSeq(weights...), k)
			got := 0
			for _, load := range loads(runs) {
				if load > got {
					got = load
				}
			}
			want := bruteForceOptimum(weights, k)
			if got != want {
				t.Errorf("weights %v k=%d: heaviest pile %d, optimal is %d (piles %v)",
					weights, k, got, want, loads(runs))
			}
		}
	}
}

// Greedy alone front-loads. The point of the minimal capacity is that the
// heaviest pile is as light as it can be even when that makes the FIRST pile
// the small one: 30|50 beats 55|25, because 50 < 55.
func TestPartitionPrefersTheLighterMaximumOverAnEvenerLook(t *testing.T) {
	got := loads(partitionLetters(letterSeq(30, 25, 20, 5), 2))
	if len(got) != 2 || got[0] != 30 || got[1] != 50 {
		t.Errorf("loads %v, want [30 50]", got)
	}
}

// When the optimal capacity lets greedy finish in fewer runs than the caller
// asked for, the spare piles are peeled off the heaviest run rather than
// returned empty.
func TestPartitionPadsToTheRequestedPileCount(t *testing.T) {
	runs := partitionLetters(letterSeq(1, 1, 1, 100), 3)
	got := loads(runs)
	if len(got) != 3 {
		t.Fatalf("got %d piles, want 3 (loads %v)", len(got), got)
	}
	for i, load := range got {
		if load == 0 || len(runs[i]) == 0 {
			t.Fatalf("empty pile %d in %v", i+1, got)
		}
	}
}

func TestPartitionFewerLettersThanPiles(t *testing.T) {
	runs := partitionLetters(letterSeq(5, 5), 7)
	if len(runs) != 2 {
		t.Fatalf("got %d piles for 2 letters, want 2", len(runs))
	}
}

func tripsFor(dropOff string, n int) []Models.TripStruct {
	out := make([]Models.TripStruct, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, Models.TripStruct{
			Company:      "Watanya",
			DropOffPoint: dropOff,
			Terminal:     "بدر",
			Date:         fmt.Sprintf("2026-07-%02d", i+1),
			ReceiptNo:    fmt.Sprintf("%s-%03d", dropOff, i),
		})
	}
	return out
}

func TestBuildPilePlanLetterMode(t *testing.T) {
	var trips []Models.TripStruct
	trips = append(trips, tripsFor("العياط", 3)...)
	trips = append(trips, tripsFor("عين شمس", 2)...)
	trips = append(trips, tripsFor("بلبيس 2", 7)...)

	plan := BuildPilePlan(trips, ModeLetter, 0)

	if plan.TotalReceipts != 12 || plan.TotalDropOffs != 3 {
		t.Fatalf("totals: %d receipts / %d drop-offs, want 12/3",
			plan.TotalReceipts, plan.TotalDropOffs)
	}
	if len(plan.Piles) != 2 {
		t.Fatalf("got %d piles, want one per present letter (2)", len(plan.Piles))
	}
	// ب sorts before ع, and العياط files under ع rather than ا.
	if plan.Piles[0].Label != "ب" || plan.Piles[1].Label != "ع" {
		t.Errorf("labels %q,%q want ب,ع", plan.Piles[0].Label, plan.Piles[1].Label)
	}
	if plan.Piles[1].ReceiptCount != 5 {
		t.Errorf("ع pile holds %d receipts, want 5", plan.Piles[1].ReceiptCount)
	}
	// Drop-offs alphabetical inside the letter.
	names := plan.Piles[1].Letters[0].DropOffs
	if names[0].Name != "العياط" || names[1].Name != "عين شمس" {
		t.Errorf("drop-offs out of order: %q, %q", names[0].Name, names[1].Name)
	}
	// Every receipt carries its pile number and its n-of-m position.
	r := names[0].Receipts[2]
	if r.Pile != 2 || r.Seq != 3 || r.OutOf != 3 {
		t.Errorf("receipt stamp: pile %d seq %d/%d, want 2 3/3", r.Pile, r.Seq, r.OutOf)
	}
}

func TestBuildPilePlanBalancedDerivesCountFromRange(t *testing.T) {
	var trips []Models.TripStruct
	for _, l := range []string{"احمد جلال", "بلبيس", "جمصه", "حلوان", "دجله", "روض الفرج"} {
		trips = append(trips, tripsFor(l, 25)...)
	}
	// 150 receipts / 50 per pile = 3 piles, chosen from the data rather than
	// from a constant the caller had to know.
	plan := BuildPilePlan(trips, ModeBalanced, 0)
	if len(plan.Piles) != 3 {
		t.Fatalf("got %d piles, want 3 (150 receipts / %d)", len(plan.Piles), DefaultTargetPerPile)
	}
	for _, p := range plan.Piles {
		if p.ReceiptCount != 50 {
			t.Errorf("pile %d holds %d, want an even 50", p.Index, p.ReceiptCount)
		}
	}
	if plan.HeaviestPile != 50 || plan.LightestPile != 50 {
		t.Errorf("spread %d..%d, want 50..50", plan.LightestPile, plan.HeaviestPile)
	}
}

func TestBuildPilePlanSkipsBlankReceipts(t *testing.T) {
	trips := tripsFor("حلوان", 2)
	trips = append(trips, Models.TripStruct{DropOffPoint: "حلوان", ReceiptNo: "  "})
	plan := BuildPilePlan(trips, ModeLetter, 0)
	if plan.SkippedReceipts != 1 {
		t.Errorf("skipped %d, want 1", plan.SkippedReceipts)
	}
	if plan.TotalReceipts != 2 {
		t.Errorf("counted %d receipts, want 2", plan.TotalReceipts)
	}
}

func TestBuildPilePlanEmptyRange(t *testing.T) {
	plan := BuildPilePlan(nil, ModeBalanced, 0)
	if len(plan.Piles) != 0 || plan.TotalReceipts != 0 {
		t.Errorf("empty range produced %d piles / %d receipts", len(plan.Piles), plan.TotalReceipts)
	}
}

// The July 2026 shape, which is where the balancing rules were argued out: one
// letter (ش) holds 72 of 515 receipts, so no split of any kind can produce a
// pile lighter than 72.
func julyLetters() []PileLetter {
	w := []struct {
		l string
		c int
	}{{"ا", 35}, {"ب", 51}, {"ج", 27}, {"ح", 20}, {"خ", 2}, {"د", 12}, {"ر", 13},
		{"ز", 15}, {"س", 6}, {"ش", 72}, {"ض", 8}, {"ع", 31}, {"غ", 6}, {"ف", 12},
		{"ق", 14}, {"ك", 15}, {"م", 33}, {"ن", 3}, {"ه", 14}, {"و", 39}, {"ي", 21},
		{"أرقام", 52}, {"غير مسجل", 14}}
	out := make([]PileLetter, 0, len(w))
	for _, x := range w {
		out = append(out, PileLetter{Letter: x.l, ReceiptCount: x.c})
	}
	return out
}

// Among the many splits that achieve the optimal maximum, the DP must pick a
// flat one. Plain greedy at the same capacity produced 34..72 on this month.
func TestPartitionPicksTheEvenestOptimalSplit(t *testing.T) {
	got := loads(partitionLetters(julyLetters(), 9))
	hi, lo := 0, 1<<30
	for _, l := range got {
		if l > hi {
			hi = l
		}
		if l < lo {
			lo = l
		}
	}
	if hi != 72 {
		t.Errorf("heaviest pile %d, want the 72 floor set by ش (loads %v)", hi, got)
	}
	// Everything except the ش box should sit in a usable band. Greedy managed a
	// 34 here; the DP has no excuse for one that thin.
	if lo < 35 {
		t.Errorf("lightest pile %d is starved (loads %v)", lo, got)
	}
}

func TestTrimIdlePilesDropsBoxesThatBuyNothing(t *testing.T) {
	letters := julyLetters()
	// 515 receipts / 50 = 11 derived boxes, but boxes 10 and 11 cannot lower
	// the 72 floor, so they only carve thin boxes off healthy ones.
	if got := trimIdlePiles(letters, 11); got != 9 {
		t.Errorf("trimmed to %d boxes, want 9", got)
	}
	// Trimming must never make the heaviest box heavier.
	if optimalCapacity(letters, 9) != optimalCapacity(letters, 11) {
		t.Error("trim changed the achievable maximum")
	}
	// One box below that, the maximum does get worse — so it must stop.
	if optimalCapacity(letters, 8) == optimalCapacity(letters, 9) {
		t.Error("8 boxes achieve the same maximum; the trim stopped too early")
	}
}

// A count the caller typed is theirs — they may own exactly that many boxes.
func TestExplicitPileCountIsNotTrimmed(t *testing.T) {
	var trips []Models.TripStruct
	for _, name := range []string{"احمد", "بلبيس", "جمصه", "حلوان", "دجله", "روض الفرج"} {
		trips = append(trips, tripsFor(name, 10)...)
	}
	plan := BuildPilePlan(trips, ModeBalanced, 6)
	if len(plan.Piles) != 6 {
		t.Errorf("got %d piles, want the 6 that were asked for", len(plan.Piles))
	}
	if plan.AutoPileCount {
		t.Error("an explicit count was reported as derived")
	}
}

func TestPlanReportsTheFloor(t *testing.T) {
	var trips []Models.TripStruct
	trips = append(trips, tripsFor("شطا1", 30)...)
	trips = append(trips, tripsFor("بلبيس", 4)...)
	plan := BuildPilePlan(trips, ModeBalanced, 0)
	if plan.FloorLetter != "ش" || plan.FloorWeight != 30 {
		t.Errorf("floor %q/%d, want ش/30", plan.FloorLetter, plan.FloorWeight)
	}
	if plan.HeaviestPile < plan.FloorWeight {
		t.Errorf("heaviest pile %d is below the floor %d", plan.HeaviestPile, plan.FloorWeight)
	}
}

/* -------------------------------------------------------------------------- */
/* Drop-off detail — must match the fee report                                 */
/* -------------------------------------------------------------------------- */

func detailTrip(terminal, date, receipt string, capacity int) Models.TripStruct {
	return Models.TripStruct{
		Company:      "Watanya",
		DropOffPoint: "حلوان 1",
		Terminal:     terminal,
		Date:         date,
		ReceiptNo:    receipt,
		TankCapacity: capacity,
		DriverName:   "سائق",
		CarNoPlate:   "ف ل ص 4317",
	}
}

// The report sorts terminal names with sort.Strings and the receipts inside
// each by date then receipt number. Both are pinned here because the drawer
// exists to mirror the workbook, and a divergence would be invisible until
// someone reconciled the two by eye.
func TestBuildDropOffDetailMatchesReportOrder(t *testing.T) {
	trips := []Models.TripStruct{
		detailTrip("هايكستب", "2026-07-20", "894505", 1000),
		detailTrip("بدر", "2026-07-14", "144653", 2000),
		detailTrip("بدر", "2026-07-02", "143958", 3000),
		// Same day as the line above it, so the receipt number decides.
		detailTrip("بدر", "2026-07-02", "143900", 1500),
		detailTrip("هايكستب", "2026-07-07", "890987", 500),
	}
	fees := map[string]FeeInfo{
		"بدر|حلوان 1":     {Distance: 120, FeeIndex: 4, ActualFee: WatanyaBandRates[4]},
		"هايكستب|حلوان 1": {Distance: 90, FeeIndex: 2, ActualFee: WatanyaBandRates[2]},
	}

	detail := BuildDropOffDetail("حلوان 1", trips, fees)

	// Terminals in byte order, exactly as sort.Strings gives the report.
	want := []string{"بدر", "هايكستب"}
	SortReportNames(want)
	if len(detail.Terminals) != 2 {
		t.Fatalf("got %d terminal tables, want 2", len(detail.Terminals))
	}
	for i, w := range want {
		if detail.Terminals[i].Terminal != w {
			t.Errorf("terminal %d is %q, want %q", i, detail.Terminals[i].Terminal, w)
		}
	}

	badr := detail.Terminals[0]
	if badr.Terminal != "بدر" {
		badr = detail.Terminals[1]
	}
	gotReceipts := make([]string, 0, len(badr.Receipts))
	for _, r := range badr.Receipts {
		gotReceipts = append(gotReceipts, r.ReceiptNo)
	}
	// 07-02 before 07-14, and within 07-02 the lower receipt number first.
	wantReceipts := []string{"143900", "143958", "144653"}
	for i, w := range wantReceipts {
		if gotReceipts[i] != w {
			t.Errorf("receipt %d is %q, want %q (got %v)", i, gotReceipts[i], w, gotReceipts)
		}
	}
	// Seq is 1-based and follows the sorted order, like the report's "#".
	for i, r := range badr.Receipts {
		if r.Seq != i+1 {
			t.Errorf("seq %d on row %d, want %d", r.Seq, i, i+1)
		}
	}
}

// Cost is litres x rate per 1,000 litres, the same arithmetic the report uses.
func TestBuildDropOffDetailPricesLikeTheReport(t *testing.T) {
	trips := []Models.TripStruct{detailTrip("بدر", "2026-07-01", "1", 20000)}
	fees := map[string]FeeInfo{
		"بدر|حلوان 1": {Distance: 120, FeeIndex: 4, ActualFee: WatanyaBandRates[4]},
	}
	detail := BuildDropOffDetail("حلوان 1", trips, fees)

	want := 20000 * 156.2 / 1000.0 // band 4
	if got := detail.Terminals[0].Receipts[0].Cost; got != want {
		t.Errorf("cost %v, want %v", got, want)
	}
	if detail.TotalCost != want || detail.TotalCapacity != 20000 {
		t.Errorf("totals %v / %d, want %v / 20000", detail.TotalCost, detail.TotalCapacity, want)
	}
}

// The report skips a terminal with no fee mapping because it cannot price it.
// The paper still exists, so the drawer keeps it, flags it, and sorts it after
// every priced table -- which leaves the priced ones in the report's order.
func TestBuildDropOffDetailKeepsUnmappedTerminalsLast(t *testing.T) {
	trips := []Models.TripStruct{
		// Sorts BEFORE بدر by byte order, so if it were not moved to the end it
		// would displace the priced table and break the mirror.
		detailTrip("الاسكندرية", "2026-07-01", "999", 1000),
		detailTrip("بدر", "2026-07-01", "111", 1000),
	}
	fees := map[string]FeeInfo{
		"بدر|حلوان 1": {Distance: 120, FeeIndex: 4, ActualFee: WatanyaBandRates[4]},
	}
	detail := BuildDropOffDetail("حلوان 1", trips, fees)

	if detail.Terminals[0].Terminal != "بدر" || detail.Terminals[0].Unmapped {
		t.Errorf("first table is %q (unmapped=%v), want the priced بدر",
			detail.Terminals[0].Terminal, detail.Terminals[0].Unmapped)
	}
	if !detail.Terminals[1].Unmapped {
		t.Error("the unpriced table is not flagged unmapped")
	}
	if detail.UnmappedReceipts != 1 {
		t.Errorf("unmapped receipts %d, want 1", detail.UnmappedReceipts)
	}
	// An unpriced table earns nothing rather than guessing a band.
	if detail.Terminals[1].TotalCost != 0 {
		t.Errorf("unpriced table costed %v, want 0", detail.Terminals[1].TotalCost)
	}
}

func TestBuildDropOffDetailSkipsBlankReceipts(t *testing.T) {
	trips := []Models.TripStruct{
		detailTrip("بدر", "2026-07-01", "111", 1000),
		detailTrip("بدر", "2026-07-02", "  ", 1000),
	}
	detail := BuildDropOffDetail("حلوان 1", trips, map[string]FeeInfo{})
	if detail.ReceiptCount != 1 {
		t.Errorf("counted %d receipts, want 1", detail.ReceiptCount)
	}
}
