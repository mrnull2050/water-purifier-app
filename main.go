package main

import (
	"fmt"
	"log"
	"os"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/robfig/cron/v3"
	"gorm.io/gorm"
)

type Device struct {
	ID                  uint      `gorm:"primaryKey" json:"id"`
	Name                string    `json:"name"`
	PhoneNumber         string    `json:"phone_number"`
	AntiscalantDate     time.Time `json:"antiscalant_date"`
	BackwashDate        time.Time `json:"backwash_date"`
	CartridgeFilterDate time.Time `json:"cartridge_filter_date"`
}

var DB *gorm.DB

func initDB() {
	var err error
	DB, err = gorm.Open(sqlite.Open("purifier.db"), &gorm.Config{})
	if err != nil {
		log.Fatal("خطا در اتصال به دیتابیس: ", err)
	}
	DB.AutoMigrate(&Device{})
}

func sendSMS(phone string, message string) {
	fmt.Printf("[SMS] [%s] ارسال شد به شماره %s: %s\n", time.Now().Format("2006-01-02 15:04:05"), phone, message)
}

func toSolarDate(t time.Time) string {
	gYear, gMonth, gDay := t.Year(), int(t.Month()), t.Day()
	gDaysInMonth := []int{31, 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31}
	jDaysInMonth := []int{31, 31, 31, 31, 31, 31, 30, 30, 30, 30, 30, 29}

	if (gYear%4 == 0 && gYear%100 != 0) || (gYear%400 == 0) {
		gDaysInMonth[1] = 29
	}
	gy := gYear - 1600
	gm := gMonth - 1
	gDayNo := 365*gy + (gy+3)/4 - (gy+99)/100 + (gy+399)/400
	for i := 0; i < gm; i++ {
		gDayNo += gDaysInMonth[i]
	}
	gDayNo += gDay - 1
	jDayNo := gDayNo - 79
	jNp := jDayNo / 12053
	jDayNo %= 12053
	jy := 979 + 33*jNp + 4*(jDayNo/1461)
	jDayNo %= 1461

	if jDayNo >= 366 {
		jy += (jDayNo - 1) / 365
		jDayNo = (jDayNo - 1) % 365
	}
	var jm, jd int
	for i := 0; i < 11; i++ {
		if jDayNo < jDaysInMonth[i] {
			jm = i + 1
			jd = jDayNo + 1
			break
		}
		jDayNo -= jDaysInMonth[i]
	}
	if jm == 0 {
		jm, jd = 12, jDayNo+1
	}
	return fmt.Sprintf("%d/%02d/%02d", jy, jm, jd)
}

func initCronJobs() {
	c := cron.New(cron.WithSeconds())
	// بررسی هر روز ساعت ۹ صبح
	c.AddFunc("0 0 9 * * *", func() {
		var devices []Device
		DB.Find(&devices)
		todayStr := time.Now().Format("2006-01-02")
		for _, d := range devices {
			if d.AntiscalantDate.Format("2006-01-02") == todayStr {
				sendSMS(d.PhoneNumber, fmt.Sprintf("امروز تاریخ آنتی اسکالنت دستگاه %s است. پس از انجام، تیک پنل را بزنید.", d.Name))
			}
			if d.BackwashDate.Format("2006-01-02") == todayStr {
				sendSMS(d.PhoneNumber, fmt.Sprintf("امروز تاریخ بک واش دستگاه %s است. پس از انجام، تیک پنل را بزنید.", d.Name))
			}
			if d.CartridgeFilterDate.Format("2006-01-02") == todayStr {
				sendSMS(d.PhoneNumber, fmt.Sprintf("امروز تاریخ تعویض فیلتر کارتریج دستگاه %s است. پس از انجام، تیک پنل را بزنید.", d.Name))
			}
		}
	})
	c.Start()
}

func main() {
	initDB()
	initCronJobs()

	app := fiber.New()
	app.Use(logger.New())

	app.Get("/", func(c *fiber.Ctx) error {
		return c.SendFile("./templates/index.html")
	})

	app.Get("/api/devices", func(c *fiber.Ctx) error {
		var devices []Device
		DB.Find(&devices)
		return c.JSON(devices)
	})

	app.Post("/api/devices", func(c *fiber.Ctx) error {
		var input struct {
			Name                string `json:"name"`
			PhoneNumber         string `json:"phone_number"`
			AntiscalantDate     string `json:"antiscalant_date"`
			BackwashDate        string `json:"backwash_date"`
			CartridgeFilterDate string `json:"cartridge_filter_date"`
		}
		if err := c.BodyParser(&input); err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "ورودی نامعتبر"})
		}

		layout := "2006-01-02"
		tAnti, _ := time.Parse(layout, input.AntiscalantDate)
		tBack, _ := time.Parse(layout, input.BackwashDate)
		tCart, _ := time.Parse(layout, input.CartridgeFilterDate)

		device := Device{
			Name:                input.Name,
			PhoneNumber:         input.PhoneNumber,
			AntiscalantDate:     tAnti,
			BackwashDate:        tBack,
			CartridgeFilterDate: tCart,
		}
		DB.Create(&device)
		return c.JSON(device)
	})

	// API برای تمدید خودکار تاریخ‌ها (موقعی که ادمین تیک می‌زند)
	app.Put("/api/devices/:id/complete", func(c *fiber.Ctx) error {
		id := c.Params("id")
		taskType := c.Query("task") // antiscalant, backwash, cartridge

		var device Device
		if err := DB.First(&device, id).Error; err != nil {
			return c.Status(404).JSON(fiber.Map{"error": "دستگاه پیدا نشد"})
		}

		// تمدید تاریخ بر اساس تسک انجام شده از همین امروز
		switch taskType {
		case "antiscalant":
			device.AntiscalantDate = time.Now().AddDate(0, 0, 1) // ۱ روز بعد
		case "backwash":
			device.BackwashDate = time.Now().AddDate(0, 0, 2)    // ۲ روز بعد
		case "cartridge":
			device.CartridgeFilterDate = time.Now().AddDate(0, 1, 0) // ۱ ماه بعد
		}

		DB.Save(&device)
		return c.JSON(device)
	})

	port := os.Getenv("PORT")
	if port == "" {
		port = "3000"
	}
	log.Fatal(app.Listen(":" + port))
}