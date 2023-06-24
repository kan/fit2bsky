package main

import (
	"fmt"
	"log"
	"os"
	"time"

	"github.com/kan/fit2bsky/bluesky"
	"github.com/kan/fit2bsky/fitbit"
	"github.com/kan/fit2bsky/sheet"
	"github.com/kelseyhightower/envconfig"
	"github.com/pkg/errors"
	"github.com/urfave/cli/v2"
)

type Config struct {
	ClientID     string `split_words:"true"`
	ClientSecret string `split_words:"true"`
	BskyHost     string `split_words:"true" default:"https://bsky.social"`
	BskyHandle   string `split_words:"true"`
	BskyPassword string `split_words:"true"`
	SheetID      string `split_words:"true"`
	SheetName    string `split_words:"true"`
}

func main() {
	var c Config
	err := envconfig.Process("f2b", &c)
	if err != nil {
		log.Fatal(err)
	}

	app := &cli.App{
		Name:  "fit2bsky",
		Usage: "Post your weight data recorded on fitbit to bluesky",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:        "date",
				Usage:       "Date weight was recorded (e.g. 2006-01-02)",
				Aliases:     []string{"d"},
				Value:       "",
				DefaultText: "today",
			},
			&cli.BoolFlag{
				Name:    "sheet",
				Aliases: []string{"s"},
				Usage:   "Write the weight in google spreadsheet",
			},
			&cli.StringFlag{
				Name:  "cell",
				Usage: `Specify the cell in which to write the weight (e.g., "A1")`,
			},
			&cli.BoolFlag{
				Name:  "dry-run",
				Usage: "Only weight data acquisition is performed.",
			},
		},
		Action: func(ctx *cli.Context) error {
			dateStr := ctx.String("date")
			date := time.Now()
			if dateStr != "" {
				date, err = time.Parse("2006-01-02", dateStr)
				if err != nil {
					return errors.WithStack(err)
				}
			}
			fc := fitbit.NewClient(c.ClientID, c.ClientSecret)
			result, err := fc.GetWeight(date)
			if err != nil {
				return err
			}

			w := result.Weights[0]
			text := fmt.Sprintf("今日の体重: %4.1fkg (BMI: %4.2f ) 体脂肪率: %4.2f%% via Fitbit\n", w.Weight, w.BMI, w.Fat)

			if ctx.Bool("sheet") {
				cell := fmt.Sprintf("%s!%s", c.SheetName, ctx.String("cell"))

				if err := sheet.WriteCell(c.SheetID, cell, w.Weight); err != nil {
					return err
				}
			}

			if ctx.Bool("dry-run") {
				fmt.Println(text)
				return nil
			}

			bsky := bluesky.NewClient(c.BskyHost, c.BskyHandle, c.BskyPassword)
			return bsky.Post(text)
		},
	}

	if err := app.Run(os.Args); err != nil {
		log.Fatalf("%+v", err)
	}
}
