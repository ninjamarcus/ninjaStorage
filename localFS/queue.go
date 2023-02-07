package localFS

// Generic implementation of a queue that may also be used as a stack

type Queue[T any] []T

func (queue *Queue[T]) empty() bool {
	return len(*queue) == 0
}

func (queue *Queue[T]) push(value T) {
	*queue = append(*queue, value)
}

func (queue *Queue[T]) pop() (value T) {
	if n := len(*queue); n > 0 {
		n--
		value = (*queue)[n]
		*queue = (*queue)[:n]
	}
	return
}

func (queue *Queue[T]) shift() (value T) {
	if len(*queue) > 0 {
		value = (*queue)[0]
		*queue = (*queue)[1:]
	}
	return
}
