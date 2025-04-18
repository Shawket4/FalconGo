package Apis

// import (
// 	"Falcon/AbstractFunctions"
// 	"Falcon/Controllers"
// 	"Falcon/Models"
// 	"encoding/json"
// 	"fmt"
// 	"log"

// 	"github.com/360EntSecGroup-Skylar/excelize"
// 	"github.com/gofiber/fiber/v2"
// )

// func GenerateFuelEventsExcelTable(c *fiber.Ctx) error {
// 	var input struct {
// 		DateFrom string `json:"DateFrom"`
// 		DateTo   string `json:"DateTo"`
// 	}
// 	if err := c.BodyParser(&input); err != nil {
// 		log.Println(err.Error())
// 		return err
// 	}
// 	var FuelEvents []Models.FuelEvent
// 	var Cars []string
// 	CarFuelEvents := make(map[string][]Models.FuelEvent)
// 	DateFrom, err := AbstractFunctions.ParseDate(input.DateFrom)
// 	_ = DateFrom
// 	if err != nil {
// 		log.Println(err.Error())
// 		return err
// 	}

// 	DateTo, err := AbstractFunctions.ParseDate(input.DateTo)
// 	_ = DateTo
// 	if err != nil {
// 		log.Println(err.Error())
// 		return err
// 	}
// 	// if err := Models.DB.Model(&Models.FuelEvent{}).Where("date BETWEEN ? AND ?", DateFrom, DateTo).Find(&FuelEvents).Error; err != nil {
// 	if err := Models.DB.Model(&Models.FuelEvent{}).Find(&FuelEvents).Error; err != nil {
// 		log.Println(err.Error())
// 		return err
// 	}

// 	for _, event := range FuelEvents {
// 		Skip := false
// 		for _, car := range Cars {
// 			if car == event.CarNoPlate {
// 				Skip = true
// 			}
// 		}
// 		if !Skip {
// 			Cars = append(Cars, event.CarNoPlate)
// 		}
// 	}
// 	for _, car := range Cars {
// 		var carFuelEvents []Models.FuelEvent
// 		for _, event := range FuelEvents {
// 			if event.CarNoPlate == car {
// 				carFuelEvents = append(carFuelEvents, event)
// 			}
// 		}
// 		CarFuelEvents[car] = carFuelEvents
// 	}

// 	headers := map[string]string{
// 		"A1": "Date",
// 		"B1": "Car No Plate",
// 		"C1": "Driver Name",
// 		"D1": "Amount Of Liters",
// 		"E1": "Price Per Liter",
// 		"F1": "Total Price",
// 		"G1": "Fuel Usage Rate",
// 		"H1": "Previous Odometer",
// 		"I1": "Current Odometer",
// 		"J1": "Kilometers",
// 		"A2": "التاريخ",
// 		"B2": "رقم السيارة",
// 		"C2": "اسم السائق",
// 		"D2": "عدد اللترات",
// 		"E2": "سعر اللتر",
// 		"F2": "سعر التفويلة",
// 		"G2": "معدل التفويل",
// 		"H2": "عداد التفويلة الماضية",
// 		"I2": "العداد الحالي",
// 		"J2": "الكيلو مترات",
// 	}

// 	file := excelize.NewFile()

// 	for _, carSheet := range Cars {
// 		file.NewSheet(carSheet)
// 		for k, v := range headers {
// 			file.SetCellValue(carSheet, k, v)
// 		}
// 		for i := 0; i < len(CarFuelEvents[carSheet]); i++ {
// 			appendFuelRow(file, carSheet, i, CarFuelEvents[carSheet])
// 		}
// 	}
// 	file.DeleteSheet("Sheet1")
// 	var filename string = fmt.Sprintf("./fuel %s:%s.xlsx", input.DateFrom, input.DateTo)
// 	err = file.SaveAs(filename)
// 	if err != nil {
// 		fmt.Println(err)
// 	}
// 	// c.Context().SetContentType("multipart/form-data")
// 	// return c.Response().SendFile("./tasks.xlsx")
// 	return c.SendFile(filename, true)
// }

// func appendFuelRow(file *excelize.File, sheet string, index int, rows []Models.FuelEvent) (fileWriter *excelize.File) {
// 	rowCount := index + 3
// 	file.SetCellValue(sheet, fmt.Sprintf("A%v", rowCount), rows[index].Date)
// 	file.SetCellValue(sheet, fmt.Sprintf("B%v", rowCount), rows[index].CarNoPlate)
// 	file.SetCellValue(sheet, fmt.Sprintf("C%v", rowCount), rows[index].DriverName)
// 	file.SetCellValue(sheet, fmt.Sprintf("D%v", rowCount), rows[index].Liters)
// 	file.SetCellValue(sheet, fmt.Sprintf("E%v", rowCount), rows[index].PricePerLiter)
// 	file.SetCellValue(sheet, fmt.Sprintf("F%v", rowCount), rows[index].Price)
// 	file.SetCellValue(sheet, fmt.Sprintf("G%v", rowCount), rows[index].FuelRate)
// 	file.SetCellValue(sheet, fmt.Sprintf("H%v", rowCount), rows[index].OdometerBefore)
// 	file.SetCellValue(sheet, fmt.Sprintf("I%v", rowCount), rows[index].OdometerAfter)
// 	file.SetCellValue(sheet, fmt.Sprintf("J%v", rowCount), rows[index].OdometerAfter-rows[index].OdometerBefore)
// 	return file
// }

// func GenerateTripsExcelTable(c *fiber.Ctx) error {
// 	Controllers.User(c)
// 	if Controllers.CurrentUser.Id != 0 {
// 		if Controllers.CurrentUser.Permission == 0 {
// 			return c.Status(fiber.StatusForbidden).SendString("You do not have permission to access this page")
// 		} else {
// 			var data struct {
// 				DateFrom string `json:"DateFrom"`
// 				DateTo   string `json:"DateTo"`
// 			}

// 			if err := c.BodyParser(&data); err != nil {
// 				log.Println(err.Error())
// 				return err
// 			}
// 			var err error
// 			headers := map[string]string{
// 				"A1": "Date",
// 				"B1": "Customer",
// 				"C1": "المستودع",
// 				"D1": "السائق",
// 				"E1": "Truck Number",
// 				"F1": "Diesel",
// 				"G1": "Gas 80",
// 				"H1": "Gas 92",
// 				"I1": "Gas 95",
// 				"J1": "مازوت",
// 				"K1": "المجموع",
// 				"L1": "المسافة",
// 				"M1": "الفئة",
// 				"N1": "التكلفة",
// 			}
// 			file := excelize.NewFile()
// 			sheet := "Trips"
// 			file.NewSheet(sheet)
// 			file.DeleteSheet("Sheet1")
// 			for k, v := range headers {
// 				file.SetCellValue(sheet, k, v)
// 			}
// 			var tripsExcel []Models.ExcelTrip
// 			var Cars []string
// 			CarTrips := make(map[string][]Models.ExcelTrip)
// 			Days := DaysBetweenDates(data.DateFrom, data.DateTo)
// 			var trips []Models.TripStruct

// 			if err := Models.DB.Model(&Models.TripStruct{}).Where("`date` BETWEEN DATE_SUB(?, INTERVAL ? DAY) AND ? ", data.DateTo, Days, data.DateTo).Preload("Route").Order("date").Find(&trips).Error; err != nil {
// 				log.Println(err)
// 				return err
// 			}

// 			for _, trip := range trips {
// 				var excelTrip Models.ExcelTrip
// 				excelTrip.Date = trip.Date
// 				excelTrip.StartTime = trip.StartTime
// 				excelTrip.EndTime = trip.EndTime
// 				excelTrip.DriverName = trip.DriverName
// 				excelTrip.TruckNo = trip.CarNoPlate
// 				excelTrip.PickUpLocation = trip.PickUpPoint
// 				excelTrip.Revenue = trip.Revenue
// 				excelTrip.Mileage = trip.Route.Mileage
// 				var StepCompleteTime struct {
// 					//{"TruckLoad": ["", "Exxon Mobile Mostrod", true], "DropOffPoints": [["", "هاي ميكس بدر", true], ["", "هاي ميكس بدر", true]]}
// 					Terminal struct {
// 						TimeStamp    string `json:"time_stamp"`
// 						TerminalName string `json:"terminal_name"`
// 						Status       bool   `json:"status"`
// 					} `json:"terminal"`
// 					DropOffPoints []struct {
// 						TimeStamp    string `json:"time_stamp"`
// 						LocationName string `json:"location_name"`
// 						Capacity     int    `json:"capacity"`
// 						GasType      string `json:"gas_type"`
// 						Status       bool   `json:"status"`
// 					} `json:"drop_off_points"`
// 				}

// 				if err := json.Unmarshal(trip.StepCompleteTimeDB, &StepCompleteTime); err != nil {
// 					log.Println(err)
// 					return err
// 				}
// 				for _, s := range StepCompleteTime.DropOffPoints {
// 					var formattedTrip Models.ExcelTrip = excelTrip
// 					formattedTrip.Customer = s.LocationName
// 					switch s.GasType {
// 					case "Diesel":
// 						formattedTrip.Diesel = float64(s.Capacity)
// 					case "Gas 80":
// 						formattedTrip.Gas80 = float64(s.Capacity)
// 					case "Gas 92":
// 						formattedTrip.Gas92 = float64(s.Capacity)
// 					case "Gas 95":
// 						formattedTrip.Gas95 = float64(s.Capacity)
// 					case "Mazoot":
// 						formattedTrip.Mazoot = float64(s.Capacity)
// 					}
// 					formattedTrip.Total = formattedTrip.Diesel + formattedTrip.Gas80 + formattedTrip.Gas92 + formattedTrip.Gas95 + formattedTrip.Mazoot
// 					tripsExcel = append(tripsExcel, formattedTrip)
// 				}
// 			}
// 			// var getTripsQuery *sql.Rows
// 			// if Controllers.CurrentUser.Permission >= 1 && Controllers.CurrentUser.Permission != 4 {
// 			// 	getTripsQuery, err = db.Query("SELECT `Date`, `start_time`, `end_time`, `Compartments`, `PickUpLocation`, `Driver Name`, `Car No Plate`, `Milage`, `FeeRate` FROM `CarProgressBars` WHERE `Transporter` = ? AND `Date` BETWEEN DATE_SUB(?, INTERVAL ? DAY) AND ? ORDER BY `Date`;", Controllers.CurrentUser.Name, data.DateTo, Days, data.DateTo)
// 			// 	if err != nil {
// 			// 		log.Println(err.Error())
// 			// 		return err
// 			// 	}
// 			// } else {
// 			// 	getTripsQuery, err = db.Query("SELECT `Date`, `start_time`, `end_time`, `Compartments`, `PickUpLocation`, `Driver Name`, `Car No Plate`, `Milage`, `FeeRate` FROM `CarProgressBars` WHERE `Date` BETWEEN DATE_SUB(?, INTERVAL ? DAY) AND ? ORDER BY `Date`;", data.DateTo, Days, data.DateTo)
// 			// 	if err != nil {
// 			// 		log.Println(err.Error())
// 			// 		return err
// 			// 	}
// 			// }
// 			// for getTripsQuery.Next() {
// 			// 	var trip Models.ExcelTrip
// 			// 	var jsonCompartments string
// 			// 	err = getTripsQuery.Scan(&trip.Date, &trip.StartTime, &trip.EndTime, &jsonCompartments, &trip.PickUpLocation, &trip.DriverName, &trip.TruckNo, &trip.Milage, &trip.FeeRate)
// 			// 	if err != nil {
// 			// 		log.Println(err.Error())
// 			// 		return err
// 			// 	}
// 			// 	var TruckCompartments [][]interface{}
// 			// 	err = json.Unmarshal([]byte(jsonCompartments), &TruckCompartments)
// 			// 	if err != nil {
// 			// 		log.Println(err.Error())
// 			// 		return err
// 			// 	}

// 			// 	for _, s := range TruckCompartments {
// 			// 		if s[1].(string) != "Empty" {
// 			// 			var tripFormatted Models.ExcelTrip = trip
// 			// 			tripFormatted.Customer = s[1].(string)
// 			// 			switch s[2] {
// 			// 			case "Diesel":
// 			// 				tripFormatted.Diesel = s[0].(float64)
// 			// 			case "Gas 80":
// 			// 				tripFormatted.Gas80 = s[0].(float64)
// 			// 			case "Gas 92":
// 			// 				tripFormatted.Gas92 = s[0].(float64)
// 			// 			case "Gas 95":
// 			// 				tripFormatted.Gas95 = s[0].(float64)
// 			// 			case "Mazoot":
// 			// 				tripFormatted.Mazoot = s[0].(float64)
// 			// 			}

// 			// 			// if s[2] == "Gas 92" {
// 			// 			// 	tripFormatted.Gas92 = int(s[0].(float64))
// 			// 			// }
// 			// 			tripFormatted.Total = tripFormatted.Diesel + tripFormatted.Gas80 + tripFormatted.Gas92 + tripFormatted.Gas95 + tripFormatted.Mazoot
// 			// 			tripFormatted.TotalFees = tripFormatted.Total / 1000 * tripFormatted.FeeRate
// 			// 			trips = append(trips, tripFormatted)
// 			// 		}
// 			// 	}
// 			// 	fmt.Println(TruckCompartments)
// 			// }

// 			for i := 0; i < len(tripsExcel); i++ {
// 				appendRowTrips(sheet, file, i, tripsExcel)
// 			}
// 			for _, event := range tripsExcel {
// 				Skip := false
// 				for _, car := range Cars {
// 					if car == event.TruckNo {
// 						Skip = true
// 					}
// 				}
// 				if !Skip {
// 					Cars = append(Cars, event.TruckNo)
// 				}
// 			}
// 			for _, car := range Cars {
// 				var carTrips []Models.ExcelTrip
// 				for _, event := range tripsExcel {
// 					if event.TruckNo == car {
// 						carTrips = append(carTrips, event)
// 					}
// 				}
// 				CarTrips[car] = carTrips
// 			}

// 			for _, carSheet := range Cars {
// 				file.NewSheet(carSheet)
// 				for k, v := range headers {
// 					file.SetCellValue(carSheet, k, v)
// 				}
// 				for i := 0; i < len(CarTrips[carSheet]); i++ {
// 					appendRowTrips(carSheet, file, i, CarTrips[carSheet])
// 				}
// 			}
// 			var filename string = fmt.Sprintf("./tasks%s:%s.xlsx", data.DateFrom, data.DateTo)
// 			err = file.SaveAs(filename)
// 			if err != nil {
// 				log.Println(err)
// 			}
// 			// c.Context().SetContentType("multipart/form-data")
// 			// return c.Response().SendFile("./tasks.xlsx")
// 			return c.SendFile(filename, true)
// 		}
// 	}
// 	return c.JSON(fiber.Map{
// 		"message": "Not Logged In.",
// 	})
// }
// func GenerateReceipt(c *fiber.Ctx) error {
// 	var data struct {
// 		Month  int     `json:"month"`
// 		Liters float64 `json:"liters"`
// 		Amount float64 `json:"amount"`
// 	}
// 	if err := c.BodyParser(&data); err != nil {
// 		log.Println(err.Error())
// 		return err
// 	}
// 	file, err := excelize.OpenFile("./Template.xlsx")
// 	if err != nil {
// 		log.Println(err.Error())
// 		return err
// 	}
// 	file.SetCellValue("Sheet1", "A12", fmt.Sprintf("%.2f", data.Amount))
// 	file.SetCellValue("Sheet1", "D12", fmt.Sprintf("%.2f", data.Liters))
// 	file.SetCellValue("Sheet1", "D13", fmt.Sprintf("%.2f", data.Amount*0.14))
// 	file.SetCellValue("Sheet1", "A13", fmt.Sprintf("%.2f", data.Amount*1.14))
// 	file.SetCellValue("Sheet1", "A17", fmt.Sprintf("%.2f", data.Amount*1.14))
// 	var filename string = fmt.Sprintf("./test.xlsx")
// 	err = file.SaveAs(filename)
// 	if err != nil {
// 		fmt.Println(err)
// 	}

// 	return c.SendFile("./test.xlsx", true)
// }

// // func GenerateCSVReceipt(c *fiber.Ctx) error {
// // 	Controllers.User(c)
// // 	if Controllers.CurrentUser.Id != 0 {
// // 		if Controllers.CurrentUser.Permission == 0 {
// // 			return c.Status(fiber.StatusForbidden).SendString("You do not have permission to access this page")
// // 		} else {
// // 			var data struct {
// // 				Month  string  `json:"Month"`
// // 				Amount float64 `json:"Amount"`
// // 			}

// // 			err := c.BodyParser(&data)

// // 			if err != nil {
// // 				log.Println(err.Error())
// // 				return err
// // 			}

// // 			headers := map[string]string{
// // 				"A1": "اسم مقاول النقل :: ماجدة عدلى ابراهيم _ مقاول نقل مواد بترولية بسىارتة وسيارات الغير",
// // 				"A2": "العنوان : قصر الباسل - اطسا - فيوم.                                                                  ت .م / 01212951212							",
// // 				"A3":  " ض : قيمة مضافة  :",
// // 				"A4":  "ملف ضريبى :",
// // 				"A5":  "فاتورة نقل مواد بترولية رقم ( 4 ) Number",
// // 				"A6":  "مطلوب  : شركة اولى انرجى - مصر ( لبيا اويل - سابقا)  ش.م. م",
// // 				"A7":  "قيمة (توقفات ) مواد بترولية طبقا للكشف المرفق خلال شهر ابريل               2022  ",
// // 				"A8":  "القيمة",
// // 				"A9":  fmt.Sprintf("%f", data.Amount),
// // 				"A10": fmt.Sprintf("%f", data.Amount*0.14),
// // 				"A11": "",
// // 				"A12": fmt.Sprintf("%f", data.Amount*1.14),
// // 				// "M1":  "التكلفة",
// // 			}
// // 			file := excelize.NewFile()
// // 			file.MergeCell("Sheet1", "A1", "H2")
// // 			for k, v := range headers {
// // 				file.SetCellValue("Sheet1", k, v)
// // 				file.SetSheetViewOptions("Sheet1", -1, excelize.RightToLeft(true))
// // 			}
// //			var filename string = fmt.Sprintf("./tasks.xlsx")
// //			err = file.SaveAs(filename)
// //			if err != nil {
// //				fmt.Println(err)
// //			}
// //			// c.Context().SetContentType("multipart/form-data")
// //			// return c.Response().SendFile("./tasks.xlsx")
// //			return c.SendFile("./tasks.xlsx", true)
// // 		}
// // 	}
// // 	return c.JSON(fiber.Map{
// // 		"message": "Not Logged In.",
// // 	})
// // 	// appendRowTrips()
// // 	// fmt.Println(tasks[1])
// // }

// func appendRowTrips(sheet string, file *excelize.File, index int, row []Models.ExcelTrip) (fileWriter *excelize.File) {
// 	rowCount := index + 2
// 	file.SetCellValue(sheet, fmt.Sprintf("A%v", rowCount), row[index].Date)
// 	file.SetCellValue(sheet, fmt.Sprintf("B%v", rowCount), row[index].Customer)
// 	file.SetCellValue(sheet, fmt.Sprintf("C%v", rowCount), row[index].PickUpLocation)
// 	file.SetCellValue(sheet, fmt.Sprintf("D%v", rowCount), row[index].DriverName)
// 	file.SetCellValue(sheet, fmt.Sprintf("E%v", rowCount), row[index].TruckNo)
// 	file.SetCellValue(sheet, fmt.Sprintf("F%v", rowCount), row[index].Diesel)
// 	file.SetCellValue(sheet, fmt.Sprintf("G%v", rowCount), row[index].Gas80)
// 	file.SetCellValue(sheet, fmt.Sprintf("H%v", rowCount), row[index].Gas92)
// 	file.SetCellValue(sheet, fmt.Sprintf("I%v", rowCount), row[index].Gas95)
// 	file.SetCellValue(sheet, fmt.Sprintf("J%v", rowCount), row[index].Mazoot)
// 	file.SetCellValue(sheet, fmt.Sprintf("K%v", rowCount), row[index].Total)
// 	file.SetCellValue(sheet, fmt.Sprintf("L%v", rowCount), row[index].Mileage)
// 	file.SetCellValue(sheet, fmt.Sprintf("M%v", rowCount), row[index].Revenue)
// 	return file

// }

// type TotalCarExpenses struct {
// 	TotalExpenses float64
// 	TotalRevenue  float64
// 	FuelEvents    []Models.FuelEvent
// 	ServiceEvents []Models.Service
// 	OilChanges    []Models.OilChange
// 	Trips         []Models.TripStruct
// }

// func GenerateExpensesExcelFile(input TotalCarExpenses) *excelize.File {
// 	fuelEventHeaders := map[string]string{
// 		"A1": "Date",
// 		"B1": "Car No Plate",
// 		"C1": "Driver Name",
// 		"D1": "Amount Of Liters",
// 		"E1": "Price Per Liter",
// 		"F1": "Total Price",
// 		"G1": "Fuel Usage Rate",
// 		"H1": "Previous Odometer",
// 		"I1": "Current Odometer",
// 		"J1": "Kilometers",
// 		"A2": "التاريخ",
// 		"B2": "رقم السيارة",
// 		"C2": "اسم السائق",
// 		"D2": "عدد اللترات",
// 		"E2": "سعر اللتر",
// 		"F2": "سعر التفويلة",
// 		"G2": "معدل التفويل",
// 		"H2": "عداد التفويلة الماضية",
// 		"I2": "العداد الحالي",
// 		"J2": "الكيلو مترات",
// 	}
// 	file := excelize.NewFile()
// 	file.NewSheet("Total")
// 	file.SetCellValue("Total", "A1", "Total Revenue")
// 	file.SetCellValue("Total", "B1", input.TotalRevenue)
// 	file.SetCellValue("Total", "A2", "Total Expenses")
// 	file.SetCellValue("Total", "B2", input.TotalExpenses)
// 	fuelSheet := "Fuel"
// 	file.NewSheet(fuelSheet)
// 	file.DeleteSheet("Sheet1")

// 	for k, v := range fuelEventHeaders {
// 		file.SetCellValue(fuelSheet, k, v)
// 	}
// 	for i := range input.FuelEvents {
// 		appendFuelRow(file, fuelSheet, i, input.FuelEvents)
// 	}

// 	serviceHeaders := map[string]string{
// 		"A1": "التاريخ",
// 		"B1": "رقم العربية",
// 		"C1": "السائق",
// 		"D1": "وصف الصيانة",
// 		"E1": "عداد الصيانة",
// 		"F1": "المشرف",
// 		"G1": "التكلفة",
// 		// "A" + index: input.DateOfService,
// 		// "B" + index: input.CarNoPlate,
// 		// "C" + index: input.DriverName,
// 		// "D" + index: input.ServiceType,
// 		// "E" + index: strconv.Itoa(input.OdometerReading),
// 		// "F" + index: input.SuperVisor,
// 		// "G" + index: fmt.Sprintf("%v", input.Cost),
// 	}

// 	serviceSheet := "Service"
// 	file.NewSheet(serviceSheet)

// 	for k, v := range serviceHeaders {
// 		file.SetCellValue(serviceSheet, k, v)
// 	}

// 	for i := range input.ServiceEvents {
// 		appendServiceRow(file, serviceSheet, i, input.ServiceEvents)
// 	}

// 	// difference := input.CurrentOdometer - input.OdometerAtChange
// 	// mileageLeft := input.Mileage - difference

// 	oilChangeHeaders := map[string]string{
// 		"A1": "التاريخ",
// 		"B1": "رقم العربية",
// 		"C1": "السائق",
// 		"D1": "عداد التغير",
// 		"E1": "العداد الحالي",
// 		"F1": "فرق الكيلومتر",
// 		"G1": "نوع الزيت",
// 		"H1": "متبقي",
// 		"I1": "المشرف",
// 		"J1": "التكلفة",
// 		// "A" + index: input.Date,
// 		// "B" + index: input.CarNoPlate,
// 		// "C" + index: input.DriverName,
// 		// "D" + index: strconv.Itoa(int(input.OdometerAtChange)),
// 		// "E" + index: strconv.Itoa(int(input.CurrentOdometer)),
// 		// "F" + index: strconv.Itoa(int(difference)),
// 		// "G" + index: strconv.Itoa(int(input.Mileage)),
// 		// "H" + index: strconv.Itoa(int(mileageLeft)),
// 		// "I" + index: input.SuperVisor,
// 		// "J" + index: fmt.Sprintf("%v", input.Cost),
// 	}
// 	oilChangeSheet := "OilChanges"
// 	file.NewSheet(oilChangeSheet)

// 	for k, v := range oilChangeHeaders {
// 		file.SetCellValue(oilChangeSheet, k, v)
// 	}

// 	for i := range input.OilChanges {
// 		appendOilChangeRow(file, oilChangeSheet, i, input.OilChanges)
// 	}

// 	tripsSheet := "Trips"
// 	file.NewSheet(tripsSheet)
// 	tripHeaders := map[string]string{
// 		"A1": "Date",
// 		"B1": "Customer",
// 		"C1": "Terminal",
// 		"D1": "Driver",
// 		"E1": "Car No Plate",
// 		"F1": "Diesel",
// 		"G1": "Gas 80",
// 		"H1": "Gas 92",
// 		"I1": "Gas 95",
// 		"J1": "Mazoot",
// 		"K1": "Total",
// 		"L1": "Distance",
// 		"M1": "Revenue",
// 	}
// 	var tripsExcel []Models.ExcelTrip
// 	for _, trip := range input.Trips {
// 		var excelTrip Models.ExcelTrip
// 		excelTrip.Date = trip.Date
// 		excelTrip.StartTime = trip.StartTime
// 		excelTrip.EndTime = trip.EndTime
// 		excelTrip.DriverName = trip.DriverName
// 		excelTrip.TruckNo = trip.CarNoPlate
// 		excelTrip.PickUpLocation = trip.PickUpPoint
// 		excelTrip.Revenue = trip.Revenue
// 		excelTrip.Mileage = trip.Mileage
// 		var StepCompleteTime struct {
// 			//{"TruckLoad": ["", "Exxon Mobile Mostrod", true], "DropOffPoints": [["", "هاي ميكس بدر", true], ["", "هاي ميكس بدر", true]]}
// 			Terminal struct {
// 				TimeStamp    string `json:"time_stamp"`
// 				TerminalName string `json:"terminal_name"`
// 				Status       bool   `json:"status"`
// 			} `json:"terminal"`
// 			DropOffPoints []struct {
// 				TimeStamp    string `json:"time_stamp"`
// 				LocationName string `json:"location_name"`
// 				Capacity     int    `json:"capacity"`
// 				GasType      string `json:"gas_type"`
// 				Status       bool   `json:"status"`
// 			} `json:"drop_off_points"`
// 		}

// 		if err := json.Unmarshal(trip.StepCompleteTimeDB, &StepCompleteTime); err != nil {
// 			log.Println(err)
// 		}
// 		for _, s := range StepCompleteTime.DropOffPoints {
// 			var formattedTrip Models.ExcelTrip = excelTrip
// 			formattedTrip.Customer = s.LocationName
// 			switch s.GasType {
// 			case "Diesel":
// 				formattedTrip.Diesel = float64(s.Capacity)
// 			case "Gas 80":
// 				formattedTrip.Gas80 = float64(s.Capacity)
// 			case "Gas 92":
// 				formattedTrip.Gas92 = float64(s.Capacity)
// 			case "Gas 95":
// 				formattedTrip.Gas95 = float64(s.Capacity)
// 			case "Mazoot":
// 				formattedTrip.Mazoot = float64(s.Capacity)
// 			}
// 			formattedTrip.Total = formattedTrip.Diesel + formattedTrip.Gas80 + formattedTrip.Gas92 + formattedTrip.Gas95 + formattedTrip.Mazoot
// 			tripsExcel = append(tripsExcel, formattedTrip)
// 		}

// 		for k, v := range tripHeaders {
// 			file.SetCellValue(tripsSheet, k, v)
// 		}

// 		for i := range tripsExcel {
// 			appendRowTrips(tripsSheet, file, i, tripsExcel)
// 		}

// 	}

// 	return file
// }

// func appendServiceRow(file *excelize.File, sheet string, index int, rows []Models.Service) (fileWriter *excelize.File) {
// 	rowCount := index + 2
// 	file.SetCellValue(sheet, fmt.Sprintf("A%v", rowCount), rows[index].DateOfService)
// 	file.SetCellValue(sheet, fmt.Sprintf("B%v", rowCount), rows[index].CarNoPlate)
// 	file.SetCellValue(sheet, fmt.Sprintf("C%v", rowCount), rows[index].DriverName)
// 	file.SetCellValue(sheet, fmt.Sprintf("D%v", rowCount), rows[index].ServiceType)
// 	file.SetCellValue(sheet, fmt.Sprintf("E%v", rowCount), rows[index].OdometerReading)
// 	file.SetCellValue(sheet, fmt.Sprintf("F%v", rowCount), rows[index].SuperVisor)
// 	file.SetCellValue(sheet, fmt.Sprintf("G%v", rowCount), rows[index].Cost)
// 	return file
// }

// func appendOilChangeRow(file *excelize.File, sheet string, index int, rows []Models.OilChange) (fileWriter *excelize.File) {
// 	difference := rows[index].CurrentOdometer - rows[index].OdometerAtChange
// 	mileageLeft := rows[index].Mileage - difference
// 	rowCount := index + 2
// 	file.SetCellValue(sheet, fmt.Sprintf("A%v", rowCount), rows[index].Date)
// 	file.SetCellValue(sheet, fmt.Sprintf("B%v", rowCount), rows[index].CarNoPlate)
// 	file.SetCellValue(sheet, fmt.Sprintf("C%v", rowCount), rows[index].DriverName)
// 	file.SetCellValue(sheet, fmt.Sprintf("D%v", rowCount), rows[index].OdometerAtChange)
// 	file.SetCellValue(sheet, fmt.Sprintf("E%v", rowCount), rows[index].CurrentOdometer)
// 	file.SetCellValue(sheet, fmt.Sprintf("F%v", rowCount), difference)
// 	file.SetCellValue(sheet, fmt.Sprintf("G%v", rowCount), rows[index].Mileage)
// 	file.SetCellValue(sheet, fmt.Sprintf("H%v", rowCount), mileageLeft)
// 	file.SetCellValue(sheet, fmt.Sprintf("I%v", rowCount), rows[index].SuperVisor)
// 	file.SetCellValue(sheet, fmt.Sprintf("J%v", rowCount), rows[index].Cost)
// 	return file
// }
