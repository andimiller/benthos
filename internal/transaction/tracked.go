package transaction

import (
	"context"
	"errors"

	"github.com/benthosdev/benthos/v4/internal/batch"
	"github.com/benthosdev/benthos/v4/internal/message"
)

// Tracked is a transaction type that adds identifying tags to messages such
// that an error returned resulting from multiple transaction messages can be
// reduced.
type Tracked struct {
	msg   message.Batch
	group *message.SortGroup
	ackFn func(context.Context, error) error
}

// NewTracked creates a transaction from a message batch and a response channel.
// The message is tagged with an identifier for the transaction, and if an error
// is returned from a downstream component that merged messages from other
// transactions the tag can be used in order to determine whether the message
// owned by this transaction succeeded.
func NewTracked(msg message.Batch, ackFn func(context.Context, error) error) *Tracked {
	group, trackedMsg := message.NewSortGroup(msg)
	return &Tracked{
		msg:   trackedMsg,
		group: group,
		ackFn: ackFn,
	}
}

// Message returns the message owned by this transaction.
func (t *Tracked) Message() message.Batch {
	return t.msg
}

func (t *Tracked) getResFromGroup(walkable batch.WalkableError) error {
	remainingIndexes := make(map[int]struct{}, t.msg.Len())
	for i := 0; i < t.msg.Len(); i++ {
		remainingIndexes[i] = struct{}{}
	}

	var res error
	walkable.WalkParts(func(_ int, p *message.Part, err error) bool {
		if index := t.group.GetIndex(p); index >= 0 {
			if err != nil {
				res = err
				return false
			}
			delete(remainingIndexes, index)
			if len(remainingIndexes) == 0 {
				return false
			}
		}
		return true
	})
	if res != nil {
		return res
	}

	if len(remainingIndexes) > 0 {
		return errors.Unwrap(walkable)
	}
	return nil
}

func (t *Tracked) resFromError(err error) error {
	if err != nil {
		if walkable, ok := err.(batch.WalkableError); ok {
			err = t.getResFromGroup(walkable)
		}
	}
	return err
}

// Ack provides a response to the upstream service from an error.
func (t *Tracked) Ack(ctx context.Context, err error) error {
	return t.ackFn(ctx, t.resFromError(err))
}
