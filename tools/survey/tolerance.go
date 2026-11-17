package main

import (
	"context"
	"database/sql"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/portedit"
	"github.com/herbygillot/dockhand/internal/macports/portedit/observe"
)

// toleranceVerdict is the product's final reader verdict, not an inference
// from experiment equality. Evaluated is false when no verdict was possible.
type toleranceVerdict struct {
	Evaluated    bool
	Inconclusive bool
	Left         bool
	ErrorCode    string `json:",omitempty"`
}

func measureTolerance(ctx context.Context, observation macports.Observation, name string, contents []byte, err error) toleranceVerdict {
	if err != nil {
		return toleranceVerdict{Left: observation.Modeled && observation.Unanswered != "", ErrorCode: portedit.Problem("evaluation", err).Code}
	}
	if observation.Unanswered != "" {
		return toleranceVerdict{Left: observation.Modeled, ErrorCode: "check-inconclusive"}
	}
	port, ok := observation.Ports[name]
	if !ok {
		return toleranceVerdict{ErrorCode: "observation-incomplete"}
	}
	_, inconclusive := observe.Tolerate(ctx, port, contents, observation.Snapshot.Root)
	return toleranceVerdict{Evaluated: true, Inconclusive: inconclusive, Left: observation.Modeled && (observation.Unanswered != "" || inconclusive)}
}

func recordTolerance(tx *sql.Tx, id int64, verdict *toleranceVerdict) error {
	if verdict == nil {
		return nil
	}
	_, err := tx.Exec(`INSERT INTO tolerance_context(evaluation_id,evaluated,inconclusive,"left",error_code) VALUES(?,?,?,?,?)`, id, verdict.Evaluated, verdict.Inconclusive, verdict.Left, verdict.ErrorCode)
	return err
}

type toleranceCounts struct {
	Evaluated, Conclusive, Inconclusive, Left, NotEvaluated int
}

func (c *toleranceCounts) add(v *toleranceVerdict) {
	if v == nil || !v.Evaluated {
		c.NotEvaluated++
	} else {
		c.Evaluated++
		if v.Inconclusive {
			c.Inconclusive++
		} else {
			c.Conclusive++
		}
	}
	if v != nil && v.Left {
		c.Left++
	}
}
