package services

import (
	"math"
	"sort"
	"strings"
	"unicode"

	"Falcon/Models"
)

// Receipt piles: turning a date range of Watanya trips into physical stacks of
// paper someone can actually sort on a desk.
//
// The problem this solves is not analytical. Once a month the receipts for a
// range come back as one heap, and they have to be filed by drop-off point.
// Sorting 500 loose sheets against a 200-name list is the slow part; sorting
// them into a handful of labelled piles first, then alphabetising inside each
// pile, is much faster. So the output is a pile plan: which first letters go in
// which pile, and which drop-off names live under each letter.
//
// Two rules shape everything below:
//
//   - The key is the FIRST LETTER of the drop-off name with the definite
//     article stripped. العياط files under ع, not ا — otherwise a third of
//     Egypt's place names land in one pile and the scheme is useless.
//   - Weight is RECEIPTS, not drop-off points. A pile's cost to sort is the
//     number of sheets you hold, and one drop-off with 40 receipts is more work
//     than ten drop-offs with one each.
//
// This module is Watanya-only by construction: the caller filters on company,
// and nothing here is parameterised by it. It is deliberately NOT part of the
// Watanya fee report in Controllers/trip.go — that report is a billing
// document with its own layout and audience, and welding a filing plan onto it
// would give both the wrong shape.

/* -------------------------------------------------------------------------- */
/* Letters                                                                     */
/* -------------------------------------------------------------------------- */

// ArabicAlphabet in the collation order the piles follow. Hamza carriers, ة and
// ى are folded onto their base letters by normaliseLetter rather than given
// slots of their own: a filer looking for أسيوط and اسيوط wants one pile.
var ArabicAlphabet = []rune{
	'ا', 'ب', 'ت', 'ث', 'ج', 'ح', 'خ', 'د', 'ذ', 'ر', 'ز', 'س', 'ش', 'ص', 'ض',
	'ط', 'ظ', 'ع', 'غ', 'ف', 'ق', 'ك', 'ل', 'م', 'ن', 'ه', 'و', 'ي',
}

// Buckets for names that have no Arabic first letter. They sort after the
// alphabet, in this order, and are labelled for a human reading the box.
const (
	BucketDigits = "أرقام"    // the name starts with a number
	BucketLatin  = "لاتيني"   // Latin script (e.g. "In garage")
	BucketUnset  = "غير مسجل" // empty / placeholder drop-off point
)

// letterRank gives every bucket a position so piles come out in reading order.
var letterRank = func() map[string]int {
	m := make(map[string]int, len(ArabicAlphabet)+3)
	for i, r := range ArabicAlphabet {
		m[string(r)] = i
	}
	m[BucketDigits] = len(ArabicAlphabet)
	m[BucketLatin] = len(ArabicAlphabet) + 1
	m[BucketUnset] = len(ArabicAlphabet) + 2
	return m
}()

// arabicFolds collapses the letter variants that must not split a pile.
var arabicFolds = map[rune]rune{
	'أ': 'ا', 'إ': 'ا', 'آ': 'ا', 'ٱ': 'ا', 'ٲ': 'ا', 'ٳ': 'ا',
	'ة': 'ه', 'ه': 'ه',
	'ى': 'ي', 'ئ': 'ي',
	'ؤ': 'و',
}

// isArabicDiacritic reports the marks that carry no filing information:
// harakat, shadda, sukun, and the tatweel used to stretch a word visually.
func isArabicDiacritic(r rune) bool {
	return (r >= 0x064B && r <= 0x0652) || r == 0x0640 || (r >= 0x0610 && r <= 0x061A)
}

// LetterOf returns the pile key for one drop-off name.
//
// The definite article is stripped BEFORE the first letter is taken, which is
// the whole point: العياط → عياط → ع. It is stripped only when something is
// left behind that can still be filed, so a hypothetical name of just "ال"
// keeps its ا rather than vanishing into the unset bucket.
func LetterOf(name string) string {
	cleaned := make([]rune, 0, len(name))
	for _, r := range name {
		if isArabicDiacritic(r) {
			continue
		}
		cleaned = append(cleaned, r)
	}
	trimmed := strings.TrimSpace(string(cleaned))
	// Leading punctuation ("/الشيخ زايد") is noise, not a filing key.
	trimmed = strings.TrimLeftFunc(trimmed, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	if trimmed == "" {
		return BucketUnset
	}

	runes := []rune(trimmed)
	// Fold before comparing, so أل and ال are both articles.
	first := runes[0]
	if f, ok := arabicFolds[first]; ok {
		first = f
	}
	if first == 'ا' && len(runes) > 2 && runes[1] == 'ل' {
		rest := strings.TrimSpace(string(runes[2:]))
		if rest != "" {
			runes = []rune(rest)
			first = runes[0]
			if f, ok := arabicFolds[first]; ok {
				first = f
			}
		}
	}

	if _, ok := letterRank[string(first)]; ok {
		return string(first)
	}
	if unicode.IsDigit(first) || (first >= '٠' && first <= '٩') {
		return BucketDigits
	}
	if first >= 'a' && first <= 'z' || first >= 'A' && first <= 'Z' {
		return BucketLatin
	}
	return BucketUnset
}

// UnsetDropOff is the placeholder FalconGo writes when a trip has no route yet.
const UnsetDropOff = "Unset"

/* -------------------------------------------------------------------------- */
/* Shapes                                                                      */
/* -------------------------------------------------------------------------- */

// PileReceipt is one sheet of paper to be filed.
type PileReceipt struct {
	ReceiptNo    string `json:"receipt_no"`
	DropOffPoint string `json:"drop_off_point"`
	Terminal     string `json:"terminal"`
	Date         string `json:"date"`
	CarNoPlate   string `json:"car_no_plate"`
	DriverName   string `json:"driver_name"`
	// Position of this receipt within its drop-off, 1-based: "3/11" on the
	// checklist tells the filer how deep the stack for this name should be.
	Seq   int `json:"seq"`
	OutOf int `json:"out_of"`
	// Pile this receipt belongs to, 1-based. Denormalised onto the row so the
	// Checklist sheet can be filtered by pile without a lookup.
	Pile int `json:"pile"`
}

// PileDropOff is one drop-off name inside a pile.
type PileDropOff struct {
	Name         string   `json:"name"`
	Letter       string   `json:"letter"`
	ReceiptCount int      `json:"receipt_count"`
	Terminals    []string `json:"terminals"`
	// Never serialised. The workbook writer reads these in-process, but the
	// JSON plan is fetched again on every mode switch and box-count step, and
	// no client reads them -- the screen shows counts, and opening a drop-off
	// calls /receipt-piles/drop-off for the priced, per-terminal version.
	// Inlining them cost 136 KB a request, most of it driver names and plates
	// going to a phone that renders none of it.
	Receipts []PileReceipt `json:"-"`
}

// PileLetter is one letter's slice of a pile — kept as its own level because
// the filer works letter by letter inside a box.
type PileLetter struct {
	Letter       string        `json:"letter"`
	ReceiptCount int           `json:"receipt_count"`
	DropOffs     []PileDropOff `json:"drop_offs"`
}

// Pile is one physical box.
type Pile struct {
	Index        int          `json:"index"`
	Label        string       `json:"label"`
	Letters      []PileLetter `json:"letters"`
	DropOffCount int          `json:"drop_off_count"`
	ReceiptCount int          `json:"receipt_count"`
}

// PilePlan is the whole answer for one filtered range.
type PilePlan struct {
	Mode          string `json:"mode"`
	StartDate     string `json:"start_date"`
	EndDate       string `json:"end_date"`
	Piles         []Pile `json:"piles"`
	TotalReceipts int    `json:"total_receipts"`
	TotalDropOffs int    `json:"total_drop_offs"`
	TotalLetters  int    `json:"total_letters"`
	// Largest and smallest pile by receipts — the two numbers that say at a
	// glance whether the split is worth using.
	HeaviestPile int `json:"heaviest_pile"`
	LightestPile int `json:"lightest_pile"`
	// The heaviest single letter, and which one. No pile can be lighter than
	// this, because a letter is never split — so it is the floor under
	// HeaviestPile, and the honest answer when someone asks why the boxes are
	// not even. In July 2026 it was ش at 72 of 515 receipts.
	FloorLetter string `json:"floor_letter"`
	FloorWeight int    `json:"floor_weight"`
	// True when PileCount was derived rather than requested.
	AutoPileCount bool `json:"auto_pile_count"`
	// Receipts the range contained but no pile could take (blank receipt
	// numbers). Reported rather than silently dropped.
	SkippedReceipts int `json:"skipped_receipts"`
}

// Pile modes.
const (
	// ModeLetter gives every present letter its own pile. Predictable labels,
	// wildly uneven sizes.
	ModeLetter = "letter"
	// ModeBalanced packs runs of consecutive letters into PileCount boxes of
	// as near equal weight as the letters allow.
	ModeBalanced = "balanced"
)

// DefaultTargetPerPile sets the pile count when the caller asks for balancing
// but names no count: enough boxes that each holds roughly this many receipts.
const DefaultTargetPerPile = 50

// MaxPiles is a sanity bound. Beyond one pile per letter plus the buckets,
// extra piles are necessarily empty.
const MaxPiles = 31

/* -------------------------------------------------------------------------- */
/* Building                                                                    */
/* -------------------------------------------------------------------------- */

// BuildPilePlan groups trips into piles.
//
// pileCount is honoured only in ModeBalanced; zero means "derive one from
// DefaultTargetPerPile", which is what makes the split track the filtered
// range instead of a number frozen at design time. A wider range gets more
// boxes, not fatter ones.
func BuildPilePlan(trips []Models.TripStruct, mode string, pileCount int) PilePlan {
	plan := PilePlan{Mode: mode}

	// Bucket by drop-off name. The name is trimmed but NOT otherwise
	// normalised: "جنيفه" and "جنيفة" are different rows in the database and
	// merging them here would hide a data problem the filer needs to see.
	byName := make(map[string][]Models.TripStruct)
	for _, t := range trips {
		if strings.TrimSpace(t.ReceiptNo) == "" {
			plan.SkippedReceipts++
			continue
		}
		name := strings.TrimSpace(t.DropOffPoint)
		if name == "" || name == UnsetDropOff {
			name = UnsetDropOff
		}
		byName[name] = append(byName[name], t)
	}

	// Assemble the drop-offs, alphabetical within their letter.
	byLetter := make(map[string][]PileDropOff)
	for name, list := range byName {
		sort.Slice(list, func(i, j int) bool {
			if list[i].Date != list[j].Date {
				return list[i].Date < list[j].Date
			}
			return list[i].ReceiptNo < list[j].ReceiptNo
		})

		letter := pileLetterOf(name)

		seenTerminals := make(map[string]bool)
		d := PileDropOff{Name: name, Letter: letter, ReceiptCount: len(list)}
		for i, t := range list {
			if t.Terminal != "" && !seenTerminals[t.Terminal] {
				seenTerminals[t.Terminal] = true
				d.Terminals = append(d.Terminals, t.Terminal)
			}
			d.Receipts = append(d.Receipts, PileReceipt{
				ReceiptNo:    t.ReceiptNo,
				DropOffPoint: name,
				Terminal:     t.Terminal,
				Date:         t.Date,
				CarNoPlate:   t.CarNoPlate,
				DriverName:   t.DriverName,
				Seq:          i + 1,
				OutOf:        len(list),
			})
		}
		sort.Strings(d.Terminals)
		byLetter[letter] = append(byLetter[letter], d)
		plan.TotalReceipts += len(list)
		plan.TotalDropOffs++
	}

	// Present letters, in collation order. Absent letters are skipped
	// entirely — an empty ث box is a box someone has to carry for nothing.
	letters := make([]PileLetter, 0, len(byLetter))
	for letter, drops := range byLetter {
		sort.Slice(drops, func(i, j int) bool { return drops[i].Name < drops[j].Name })
		l := PileLetter{Letter: letter, DropOffs: drops}
		for _, d := range drops {
			l.ReceiptCount += d.ReceiptCount
		}
		letters = append(letters, l)
	}
	sort.Slice(letters, func(i, j int) bool {
		return letterRank[letters[i].Letter] < letterRank[letters[j].Letter]
	})
	plan.TotalLetters = len(letters)
	for _, l := range letters {
		if l.ReceiptCount > plan.FloorWeight {
			plan.FloorWeight, plan.FloorLetter = l.ReceiptCount, l.Letter
		}
	}

	if len(letters) == 0 {
		plan.Mode = mode
		return plan
	}

	// Cut the letter sequence into piles.
	var cuts [][]PileLetter
	if mode == ModeBalanced {
		if pileCount <= 0 {
			plan.AutoPileCount = true
			pileCount = (plan.TotalReceipts + DefaultTargetPerPile - 1) / DefaultTargetPerPile
		}
		if pileCount < 1 {
			pileCount = 1
		}
		if pileCount > len(letters) {
			pileCount = len(letters)
		}
		if pileCount > MaxPiles {
			pileCount = MaxPiles
		}
		if plan.AutoPileCount {
			pileCount = trimIdlePiles(letters, pileCount)
		}
		cuts = partitionLetters(letters, pileCount)
		plan.Mode = ModeBalanced
	} else {
		cuts = make([][]PileLetter, 0, len(letters))
		for _, l := range letters {
			cuts = append(cuts, []PileLetter{l})
		}
		plan.Mode = ModeLetter
	}

	plan.LightestPile = -1
	for i, run := range cuts {
		p := Pile{Index: i + 1, Letters: run}
		labels := make([]string, 0, len(run))
		for _, l := range run {
			labels = append(labels, l.Letter)
			p.ReceiptCount += l.ReceiptCount
			p.DropOffCount += len(l.DropOffs)
		}
		p.Label = strings.Join(labels, " ")
		if p.ReceiptCount > plan.HeaviestPile {
			plan.HeaviestPile = p.ReceiptCount
		}
		if plan.LightestPile < 0 || p.ReceiptCount < plan.LightestPile {
			plan.LightestPile = p.ReceiptCount
		}
		plan.Piles = append(plan.Piles, p)
	}
	if plan.LightestPile < 0 {
		plan.LightestPile = 0
	}

	// Stamp the pile number onto every receipt, so the checklist rows carry it.
	for pi := range plan.Piles {
		for li := range plan.Piles[pi].Letters {
			for di := range plan.Piles[pi].Letters[li].DropOffs {
				rs := plan.Piles[pi].Letters[li].DropOffs[di].Receipts
				for ri := range rs {
					rs[ri].Pile = plan.Piles[pi].Index
				}
			}
		}
	}

	return plan
}

/* -------------------------------------------------------------------------- */
/* The balancing algorithm                                                     */
/* -------------------------------------------------------------------------- */

// trimIdlePiles drops boxes that do not make the heaviest box any lighter.
//
// The derived count is total/target, which assumes the weight can actually be
// spread that thin. It often cannot: one heavy letter sets a floor under the
// heaviest pile (see PilePlan.FloorWeight), and once the split has reached that
// floor, every EXTRA box has nothing useful left to take. It carves a thin box
// off a healthy one and the plan gets worse, not better — July 2026 at 11 boxes
// ran 21..72, where the same month at 9 ran 35..72 with the same maximum.
//
// So walk the count down while the optimal maximum is unchanged: the fewest
// boxes that still achieve the best possible heaviest box. Fewer boxes to
// label, carry and store, and no loss at all.
//
// Only applied to a derived count. A count the caller typed is honoured as
// typed — they may have exactly eleven boxes on the shelf.
func trimIdlePiles(letters []PileLetter, k int) int {
	if k <= 1 {
		return k
	}
	best := optimalCapacity(letters, k)
	for k > 1 && optimalCapacity(letters, k-1) == best {
		k--
	}
	return k
}

// optimalCapacity is the weight of the heaviest pile in the best possible split
// into k consecutive runs — the binary search from partitionLetters, without
// the cutting.
func optimalCapacity(letters []PileLetter, k int) int {
	weights := letterWeights(letters)
	if k >= len(weights) {
		lo := 0
		for _, w := range weights {
			if w > lo {
				lo = w
			}
		}
		return lo
	}
	lo, hi := 0, 0
	for _, w := range weights {
		if w > lo {
			lo = w
		}
		hi += w
	}
	for lo < hi {
		mid := lo + (hi-lo)/2
		if runsNeeded(weights, mid) <= k {
			hi = mid
		} else {
			lo = mid + 1
		}
	}
	return lo
}

// letterWeights is the weight vector the partitioner works on. A letter with no
// receipts still occupies a slot in the label, so it weighs 1 rather than 0 —
// otherwise a run of empty letters could absorb a whole pile for nothing.
func letterWeights(letters []PileLetter) []int {
	weights := make([]int, len(letters))
	for i, l := range letters {
		if weights[i] = l.ReceiptCount; weights[i] == 0 {
			weights[i] = 1
		}
	}
	return weights
}

// partitionLetters splits the letter sequence into exactly k CONSECUTIVE runs,
// minimising the heaviest run.
//
// The constraint that makes this tractable is that the runs must be
// consecutive. A pile labelled "ح خ د ذ ر ز" is findable; a pile labelled
// "ح ص و" is not, because the filer holding a sheet for صفط would have to
// consult a table to learn which box it belongs to. So this is NOT bin packing
// (NP-hard, and it would shuffle letters between boxes); it is the linear
// partition problem, which has an exact answer.
//
// The method is binary search on the answer:
//
//  1. Any achievable heaviest-pile weight C lies between max(letter) — one
//     letter cannot be split, so no pile can be lighter than the heaviest
//     single letter — and sum(letters), which is one pile holding everything.
//  2. For a candidate C, `runsNeeded` walks the letters once, greedily filling
//     the current pile until the next letter would push it past C, then
//     starting a new one. That greedy pass uses the FEWEST piles possible for
//     that C: closing a pile earlier than forced can never reduce the count.
//  3. So "C is achievable with k piles" is monotone — if C works, every larger
//     C works — and binary search over the integer range finds the smallest
//     achievable C in ~log2(sum) iterations.
//  4. One final greedy pass at that C produces the actual cuts.
//
// Cost is O(n log S) for n letters and total weight S — for 28 letters and a
// few thousand receipts, microseconds. The result is optimal, not heuristic:
// no other consecutive split into k piles has a lighter heaviest pile.
//
// The greedy pass is then re-balanced backwards (see `relaxTail`) because the
// minimal C alone does not pin down where the slack sits: greedy front-loads,
// which can leave the last pile nearly empty even though the maximum is
// already optimal.
func partitionLetters(letters []PileLetter, k int) [][]PileLetter {
	n := len(letters)
	if k >= n {
		out := make([][]PileLetter, 0, n)
		for _, l := range letters {
			out = append(out, []PileLetter{l})
		}
		return out
	}
	if k <= 1 {
		return [][]PileLetter{letters}
	}

	weights := letterWeights(letters)
	lo, hi := 0, 0
	for _, w := range weights {
		if w > lo {
			lo = w
		}
		hi += w
	}

	for lo < hi {
		mid := lo + (hi-lo)/2
		if runsNeeded(weights, mid) <= k {
			hi = mid
		} else {
			lo = mid + 1
		}
	}
	capacity := lo

	// The capacity alone does not pin down the split: once the heaviest letter
	// fixes the maximum, MANY cuts achieve it, and most of them look terrible.
	// evenestCut picks the flattest one among them.
	bounds := evenestCut(weights, capacity, k)

	out := make([][]PileLetter, 0, len(bounds))
	start := 0
	for _, end := range bounds {
		out = append(out, letters[start:end])
		start = end
	}
	return out
}

// runsNeeded is the feasibility test: how many piles a greedy left-to-right
// fill takes when no pile may exceed capacity.
func runsNeeded(weights []int, capacity int) int {
	runs, load := 1, 0
	for _, w := range weights {
		if load+w > capacity {
			runs++
			load = 0
		}
		load += w
	}
	return runs
}

// evenestCut chooses, among every split into exactly k consecutive runs with
// no run heavier than `capacity`, the one whose piles are most equal.
//
// This second objective is not a nicety. The binary search above minimises the
// HEAVIEST pile, and on real data that maximum is usually pinned by one huge
// letter — ش was 72 of 515 receipts in July 2026, so no split of any kind can
// have a pile lighter than 72. Every cut that reaches 72 is "optimal" by the
// first objective, and a plain greedy pass takes whichever one it stumbles
// into: 35|51|61|34|72|71|65|60|66 on that month. The piles are optimal and
// look shambolic, which is not a useful thing to hand someone.
//
// So among the winners, prefer the flattest. Cost is measured as the sum of
// squared distances from the mean — squared, not absolute, because it is the
// OUTLIER pile that hurts (one 20-receipt box next to one 70-receipt box is
// worse than two 45s, and only a squared term says so).
//
// The DP is the textbook one: best[i][j] is the cheapest way to split the first
// i letters into j runs. Filling it is O(n²k) — with 23 letters and at most 31
// piles that is a few thousand operations, so the exact answer is cheaper than
// any heuristic would be to justify.
func evenestCut(weights []int, capacity, k int) []int {
	n := len(weights)

	// prefix[i] = weight of the first i letters, so any run's load is O(1).
	prefix := make([]int, n+1)
	for i, w := range weights {
		prefix[i+1] = prefix[i] + w
	}
	mean := float64(prefix[n]) / float64(k)

	const inf = math.MaxFloat64
	// best[i][j], and from[i][j] = where run j started, for reconstruction.
	best := make([][]float64, n+1)
	from := make([][]int, n+1)
	for i := range best {
		best[i] = make([]float64, k+1)
		from[i] = make([]int, k+1)
		for j := range best[i] {
			best[i][j] = inf
		}
	}
	best[0][0] = 0

	for i := 1; i <= n; i++ {
		for j := 1; j <= k && j <= i; j++ {
			// The last run is letters [start, i). It must be non-empty, must
			// leave at least one letter for each earlier run, and must fit.
			// Walking start DOWNWARD grows that run, so the first over-capacity
			// start ends the search — every smaller one is heavier still.
			for start := i - 1; start >= j-1; start-- {
				load := prefix[i] - prefix[start]
				if load > capacity {
					break
				}
				if best[start][j-1] == inf {
					continue
				}
				d := float64(load) - mean
				if cost := best[start][j-1] + d*d; cost < best[i][j] {
					best[i][j] = cost
					from[i][j] = start
				}
			}
		}
	}

	// The binary search proved a k-run split at this capacity exists, so this
	// cell is filled. Falling back to one run per letter would be a silent
	// wrong answer rather than a crash, hence the explicit guard.
	if best[n][k] == inf {
		return greedyCut(weights, capacity)
	}

	bounds := make([]int, k)
	for i, j := n, k; j > 0; j-- {
		bounds[j-1] = i
		i = from[i][j]
	}
	return bounds
}

// greedyCut is the plain left-to-right fill, kept only as the unreachable
// fallback in evenestCut.
func greedyCut(weights []int, capacity int) []int {
	var bounds []int
	load := 0
	for i, w := range weights {
		if load+w > capacity {
			bounds = append(bounds, i)
			load = 0
		}
		load += w
	}
	return append(bounds, len(weights))
}

/* -------------------------------------------------------------------------- */
/* Report ordering                                                             */
/*                                                                            */
/* The Watanya fee report (Controllers/trip.go) lays a drop-off out as one     */
/* table per terminal, and the drop-off drawer on the piles screen mirrors     */
/* that exactly — a filer working from the screen and a clerk working from the */
/* workbook must see the same receipts in the same order, or the screen is     */
/* worse than useless.                                                        */
/*                                                                            */
/* These are the report's rules, extracted so both callers share one           */
/* definition rather than two copies that agree today. wtBuildGroups calls     */
/* them too; a change here changes both.                                       */
/* -------------------------------------------------------------------------- */

// SortReportNames orders drop-off or terminal names the way the report does.
//
// Plain byte-order sort, NOT a locale collation. That is what `sort.Strings`
// gives and therefore what the workbook has always shown; switching to Arabic
// collation would reorder every table in a report people reconcile by eye.
func SortReportNames(names []string) {
	sort.Strings(names)
}

// pileLetterOf is LetterOf plus the placeholder rule the plan applies: the
// "Unset" drop-off is filed as unrecorded, not as a Latin name.
func pileLetterOf(name string) string {
	name = strings.TrimSpace(name)
	if name == "" || name == UnsetDropOff {
		return BucketUnset
	}
	return LetterOf(name)
}

// SortPileOrderDropOffs orders drop-off names the way the pile plan does:
// by the filing letter first (definite article stripped, variants folded —
// العياط files under ع), then by name inside that letter.
//
// The workbook and the piles screen are worked through side by side, so the
// report's drop-off tables follow the boxes rather than a raw byte sort that
// would file every ال- name under ا.
func SortPileOrderDropOffs(names []string) {
	sort.Slice(names, func(i, j int) bool {
		li, lj := letterRank[pileLetterOf(names[i])], letterRank[pileLetterOf(names[j])]
		if li != lj {
			return li < lj
		}
		return names[i] < names[j]
	})
}

// SortReportTrips orders the receipts inside one terminal's table: by date,
// then by receipt number. Both are string comparisons — dates are stored as
// YYYY-MM-DD text, which sorts chronologically, and receipt numbers are not
// always numeric.
func SortReportTrips(trips []Models.TripStruct) {
	sort.SliceStable(trips, func(i, j int) bool {
		if trips[i].Date != trips[j].Date {
			return trips[i].Date < trips[j].Date
		}
		return trips[i].ReceiptNo < trips[j].ReceiptNo
	})
}

/* -------------------------------------------------------------------------- */
/* One drop-off, laid out as the report lays it out                            */
/* -------------------------------------------------------------------------- */

// WatanyaBandRates maps a fee band (1..15) to its EGP per 1,000 litres. Same
// table as the report's wtFeeIndexToAmount; a band outside it earns nothing,
// which is deliberate — an unpriced band is a data-entry error, and inventing
// a rate would quietly bill a guess.
var WatanyaBandRates = map[int]float64{
	1: 104.5, 2: 122.1, 3: 129.8, 4: 156.2, 5: 183.7,
	6: 196.9, 7: 210.1, 8: 235.4, 9: 261.8, 10: 288.2,
	11: 314.6, 12: 341.0, 13: 367.4, 14: 393.8, 15: 420.2,
}

// Where a receipt physically is, from its most recent step.
const (
	ReceiptAtOffice = "office"
	ReceiptAtGarage = "garage"
	ReceiptNotFiled = "none"
)

// DropOffReceipt is one row of a terminal's table, carrying the same columns
// the report prints.
type DropOffReceipt struct {
	Seq          int     `json:"seq"`
	Date         string  `json:"date"`
	ReceiptNo    string  `json:"receipt_no"`
	DriverName   string  `json:"driver_name"`
	CarNoPlate   string  `json:"car_no_plate"`
	TankCapacity int     `json:"tank_capacity"`
	Mileage      float64 `json:"mileage"`
	// Where the paper is: "office", "garage", or "none" when no step has been
	// recorded. This is the filer's own bookkeeping, so it is not money and is
	// served at every permission level.
	Status string `json:"status"`
	// Money. Absent, not zeroed, below the financial permission -- a zero
	// reads as "earned nothing" where absence reads as "not for you".
	ActualFee *float64 `json:"actual_fee,omitempty"`
	Cost      *float64 `json:"cost,omitempty"`
}

// DropOffTerminalTable is one terminal's table within a drop-off.
type DropOffTerminalTable struct {
	Terminal string  `json:"terminal"`
	Distance float64 `json:"distance"`
	// The band NUMBER (1..15) is route metadata, not a price -- it says which
	// tariff row applies, not what anyone is paid -- so it is served at every
	// level. The rate it converts to is money and is not.
	FeeIndex  int      `json:"fee_index"`
	ActualFee *float64 `json:"actual_fee,omitempty"`
	/**
	 * Absent from the workbook entirely: the terminal↔drop-off pair has no fee
	 * mapping, so the report skips it. The paper still exists, so it is
	 * reported here and flagged rather than dropped.
	 */
	Unmapped      bool             `json:"unmapped"`
	Receipts      []DropOffReceipt `json:"receipts"`
	ReceiptCount  int              `json:"receipt_count"`
	TotalCapacity int              `json:"total_capacity"`
	TotalCost     *float64         `json:"total_cost,omitempty"`
	// Counts by where the paper is, so a drop-off can be read at a glance
	// without walking its rows.
	AtOffice int `json:"at_office"`
	AtGarage int `json:"at_garage"`
	NotFiled int `json:"not_filed"`
}

// DropOffDetail is the whole answer for one drop-off in one range.
type DropOffDetail struct {
	DropOffPoint  string                 `json:"drop_off_point"`
	StartDate     string                 `json:"start_date"`
	EndDate       string                 `json:"end_date"`
	Terminals     []DropOffTerminalTable `json:"terminals"`
	ReceiptCount  int                    `json:"receipt_count"`
	TotalCapacity int                    `json:"total_capacity"`
	TotalCost     *float64               `json:"total_cost,omitempty"`
	AtOffice      int                    `json:"at_office"`
	AtGarage      int                    `json:"at_garage"`
	NotFiled      int                    `json:"not_filed"`
	/** Receipts in tables the report omits — the sum over unmapped terminals. */
	UnmappedReceipts int `json:"unmapped_receipts"`
}

// BuildDropOffDetail groups one drop-off's trips into per-terminal tables in
// the report's order.
//
// The ONE deliberate difference from the workbook: the report drops a trip
// whose terminal↔drop-off pair has no fee mapping, because it cannot price it.
// Dropping it here would be a filing error — the receipt is in the box whether
// or not anyone can bill it — so those tables are kept, marked `unmapped`, and
// sorted last so the priced tables read in exactly the report's order above
// them.
//
// `statusByTrip` maps a trip id to "office" / "garage" / "none"; a trip absent
// from it has no steps and reads as not filed. `withMoney` is the permission
// gate: when false the fee and cost fields are left nil and never serialised,
// while the band NUMBER and the distance stay, because they are route
// metadata rather than a price.
func BuildDropOffDetail(
	dropOff string,
	trips []Models.TripStruct,
	feeByKey map[string]FeeInfo,
	statusByTrip map[uint]string,
	withMoney bool,
) DropOffDetail {
	detail := DropOffDetail{DropOffPoint: dropOff}

	byTerminal := make(map[string][]Models.TripStruct)
	for _, t := range trips {
		if strings.TrimSpace(t.ReceiptNo) == "" {
			continue
		}
		byTerminal[t.Terminal] = append(byTerminal[t.Terminal], t)
	}

	names := make([]string, 0, len(byTerminal))
	for name := range byTerminal {
		names = append(names, name)
	}
	SortReportNames(names)

	var mapped, unmapped []DropOffTerminalTable
	for _, terminal := range names {
		list := byTerminal[terminal]
		SortReportTrips(list)

		fee, priced := feeByKey[terminal+"|"+dropOff]
		table := DropOffTerminalTable{
			Terminal: terminal,
			Distance: fee.Distance,
			FeeIndex: fee.FeeIndex,
			Unmapped: !priced,
		}
		if withMoney {
			rate := fee.ActualFee
			table.ActualFee = &rate
			zero := 0.0
			table.TotalCost = &zero
		}
		for i, t := range list {
			status := statusByTrip[t.ID]
			if status == "" {
				status = ReceiptNotFiled
			}
			row := DropOffReceipt{
				Seq:          i + 1,
				Date:         t.Date,
				ReceiptNo:    t.ReceiptNo,
				DriverName:   t.DriverName,
				CarNoPlate:   t.CarNoPlate,
				TankCapacity: t.TankCapacity,
				Mileage:      t.Mileage,
				Status:       status,
			}
			if withMoney {
				// Same arithmetic as the report: litres × rate per 1,000 litres.
				rate := fee.ActualFee
				cost := float64(t.TankCapacity) * rate / 1000.0
				row.ActualFee = &rate
				row.Cost = &cost
				*table.TotalCost += cost
			}
			switch status {
			case ReceiptAtOffice:
				table.AtOffice++
			case ReceiptAtGarage:
				table.AtGarage++
			default:
				table.NotFiled++
			}
			table.Receipts = append(table.Receipts, row)
			table.TotalCapacity += t.TankCapacity
		}
		table.ReceiptCount = len(table.Receipts)

		if priced {
			mapped = append(mapped, table)
		} else {
			unmapped = append(unmapped, table)
		}
	}

	detail.Terminals = append(mapped, unmapped...)
	if withMoney {
		zero := 0.0
		detail.TotalCost = &zero
	}
	for _, table := range detail.Terminals {
		detail.ReceiptCount += table.ReceiptCount
		detail.TotalCapacity += table.TotalCapacity
		detail.AtOffice += table.AtOffice
		detail.AtGarage += table.AtGarage
		detail.NotFiled += table.NotFiled
		if withMoney && table.TotalCost != nil {
			*detail.TotalCost += *table.TotalCost
		}
		if table.Unmapped {
			detail.UnmappedReceipts += table.ReceiptCount
		}
	}
	return detail
}

// FeeInfo is the priced route metadata a table carries.
type FeeInfo struct {
	Distance  float64
	FeeIndex  int
	ActualFee float64
}
