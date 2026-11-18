package mutation

import (
	"context"
	"fmt"
	"path/filepath"
)

// PreparedFile links a staged file's identity to its operation in the intent.
type PreparedFile struct {
	Op    int       `json:"op"`
	Proof FileStamp `json:"proof"`
}

func runWrites(ctx context.Context, in Intent, kind Kind, prog *Progress, save func([]PreparedFile) error, steps []Step) ([]Step, error) {
	var group []int
	parent := ""
	for i, op := range in.Plan {
		if op.Kind != kind {
			continue
		}
		if save == nil {
			step, err := writeWithoutJournal(ctx, in, i, prog)
			if err != nil {
				return steps, err
			}
			crashPoint()
			steps = append(steps, step)
			continue
		}
		nextParent := filepath.Dir(plannedDestination(in.Dir, op))
		// A later write may need the previous write's destination as its parent.
		if len(group) != 0 && nextParent != parent {
			var err error
			steps, err = writeGroup(ctx, in, group, prog, save, steps)
			if err != nil {
				return steps, err
			}
			group = group[:0]
		}
		parent, group = nextParent, append(group, i)
	}
	if len(group) == 0 {
		return steps, nil
	}
	return writeGroup(ctx, in, group, prog, save, steps)
}

func writeWithoutJournal(ctx context.Context, in Intent, i int, prog *Progress) (Step, error) {
	op, planned := in.Plan[i], in.Ops[i]
	if op.Kind == OpCreate {
		c, err := Create(in.Dir, op.To)
		if err != nil {
			return Step{}, err
		}
		return took(OpCreate, "", c.Path, planned.Made), nil
	}
	from, to := Target(in.Dir, op.From), Target(in.Dir, op.To)
	proof, err := copyEntry(ctx, from, planned.Temp, to, prog)
	if err != nil {
		return Step{}, fmt.Errorf("copy %q to %q: %w", op.From, op.To, err)
	}
	return Step{Kind: OpCopy, From: from, To: to, Made: planned.Made, ID: proof.ID, Birth: proof.Birth}, nil
}

func writeGroup(ctx context.Context, in Intent, group []int, prog *Progress, save func([]PreparedFile) error, steps []Step) ([]Step, error) {
	prepared := make([]PreparedFile, 0, len(group))
	var stageErr error
	for _, i := range group {
		if stageErr = ctx.Err(); stageErr != nil {
			break
		}
		op, planned := in.Plan[i], in.Ops[i]
		if op.Kind == OpCopy {
			stageErr = stageCopy(ctx, Target(in.Dir, op.From), planned.Temp, Target(in.Dir, op.To), prog)
			if stageErr != nil {
				stageErr = fmt.Errorf("copy %q to %q: %w", op.From, op.To, stageErr)
			}
		} else {
			stageErr = stageCreate(in.Dir, op.To, planned.Temp)
		}
		if stageErr != nil {
			break
		}
		proof, err := stamp(planned.Temp)
		if err != nil {
			discard(planned.Temp)
			stageErr = err
			break
		}
		prepared = append(prepared, PreparedFile{Op: i, Proof: proof})
	}
	if len(prepared) == 0 {
		return steps, stageErr
	}
	clean := func(pending []PreparedFile) {
		for _, entry := range pending {
			discard(in.Ops[entry.Op].Temp)
		}
	}
	if err := ctx.Err(); err != nil {
		clean(prepared)
		return steps, err
	}
	if err := save(prepared); err != nil {
		clean(prepared)
		return steps, err
	}
	for i, entry := range prepared {
		op, planned := in.Plan[entry.Op], in.Ops[entry.Op]
		to := plannedDestination(in.Dir, op)
		if err := publishStaged(ctx, planned.Temp, to); err != nil {
			clean(prepared[i:])
			return steps, fmt.Errorf("%q: %w", to, err)
		}
		crashPoint()
		from := ""
		if op.Kind == OpCopy {
			from = Target(in.Dir, op.From)
		}
		steps = append(steps, Step{Kind: op.Kind, From: from, To: to, Made: planned.Made, ID: entry.Proof.ID, Birth: entry.Proof.Birth})
	}
	return steps, stageErr
}
