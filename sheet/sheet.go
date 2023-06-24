package sheet

import (
	"context"

	"github.com/pkg/errors"
	"google.golang.org/api/sheets/v4"
)

func WriteCell(sheetID, cell string, value any) error {
	srv, err := sheets.NewService(context.Background())
	if err != nil {
		return errors.WithStack(err)
	}

	var vr sheets.ValueRange
	vr.Values = append(vr.Values, []interface{}{value})

	_, err = srv.Spreadsheets.Values.Update(sheetID, cell, &vr).ValueInputOption("RAW").Do()
	if err != nil {
		return errors.WithStack(err)
	}

	return nil
}
