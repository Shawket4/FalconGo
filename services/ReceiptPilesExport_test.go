package services

import (
	"os"
	"testing"

	"Falcon/Models"
)

func TestRenderPilePlanExcelSmoke(t *testing.T) {
	var trips []Models.TripStruct
	for name, n := range map[string]int{
		"العياط": 5, "عين شمس": 2, "بلبيس 2": 7, "اسيوط الغربي1": 8,
		"حلوان 1": 13, "دجله بالميز": 12, "6 اكتوبر": 3, "In garage": 1,
		"جمصه": 4, "روض الفرج 1": 5, "الشيخ زايد 2": 6, "وادي حوف": 9,
	} {
		trips = append(trips, tripsFor(name, n)...)
	}
	plan := BuildPilePlan(trips, ModeBalanced, 0)
	plan.StartDate, plan.EndDate = "2026-07-01", "2026-07-31"
	blob, err := RenderPilePlanExcel(plan)
	if err != nil {
		t.Fatal(err)
	}
	if len(blob) < 5000 {
		t.Fatalf("workbook is only %d bytes", len(blob))
	}
	if out := os.Getenv("PILE_XLSX_OUT"); out != "" {
		if err := os.WriteFile(out, blob, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	t.Logf("piles=%d receipts=%d heaviest=%d lightest=%d",
		len(plan.Piles), plan.TotalReceipts, plan.HeaviestPile, plan.LightestPile)
	for _, p := range plan.Piles {
		t.Logf("  box %d [%s] %d names %d receipts", p.Index, p.Label, p.DropOffCount, p.ReceiptCount)
	}
}
