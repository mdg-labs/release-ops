package updateprs

// Notifier is the in-process wake signal of the ticket engine: every committed change of an
// update PR fires it, and the engine, which reads update_prs when it runs, receives from C.
// Wakes that arrive while one is pending merge into it, so a burst of changes runs the engine
// once more, not once each.
type Notifier struct {
	ch chan struct{}
}

// NewNotifier returns a Notifier with no wake pending.
func NewNotifier() *Notifier {
	return &Notifier{ch: make(chan struct{}, 1)}
}

// Wake signals that update_prs changed. It never blocks; on a nil Notifier it does nothing.
func (n *Notifier) Wake() {
	if n == nil {
		return
	}
	select {
	case n.ch <- struct{}{}:
	default:
	}
}

// C delivers a value after Wake. It is nil on a nil Notifier, which never delivers.
func (n *Notifier) C() <-chan struct{} {
	if n == nil {
		return nil
	}
	return n.ch
}
