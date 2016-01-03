package sony

type responseQueue struct {
	channels []chan error
}

func (q *responseQueue) Push(ch chan error) {
	q.channels = append(q.channels, ch)
}

func (q *responseQueue) Pop() chan error {
	if len(q.channels) == 0 {
		return nil
	}
	ch := q.channels[0]
	q.channels = q.channels[1:]
	return ch
}
