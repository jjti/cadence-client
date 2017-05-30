// Copyright (c) 2017 Uber Technologies, Inc.
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN
// THE SOFTWARE.

package cadence

import (
	"errors"
	"fmt"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDispatcher(t *testing.T) {
	value := "foo"
	d := newDispatcher(background, func(ctx Context) { value = "bar" })
	require.Equal(t, "foo", value)
	d.ExecuteUntilAllBlocked()
	require.True(t, d.IsDone())
	require.Equal(t, "bar", value)
}

func TestNonBlockingChildren(t *testing.T) {
	var history []string
	d := newDispatcher(background, func(ctx Context) {
		for i := 0; i < 10; i++ {
			ii := i
			Go(ctx, func(ctx Context) {
				history = append(history, fmt.Sprintf("child-%v", ii))
			})
		}
		history = append(history, "root")
	})
	require.EqualValues(t, 0, len(history))
	d.ExecuteUntilAllBlocked()
	require.True(t, d.IsDone())

	require.EqualValues(t, 11, len(history))
}

func TestNonbufferedChannel(t *testing.T) {
	var history []string
	d := newDispatcher(background, func(ctx Context) {
		c1 := NewChannel(ctx)
		Go(ctx, func(ctx Context) {
			history = append(history, "child-start")
			var v string
			more := c1.Receive(ctx, &v)
			assert.True(t, more)
			history = append(history, fmt.Sprintf("child-end-%v", v))
		})
		history = append(history, "root-before-channel-put")
		c1.Send(ctx, "value1")
		history = append(history, "root-after-channel-put")

	})
	require.EqualValues(t, 0, len(history))
	d.ExecuteUntilAllBlocked()
	require.True(t, d.IsDone())

	expected := []string{
		"root-before-channel-put",
		"child-start",
		"child-end-value1",
		"root-after-channel-put",
	}
	require.EqualValues(t, expected, history)

}

func TestNonbufferedChannelBlockedReceive(t *testing.T) {
	var history []string
	var c2 Channel
	d := newDispatcher(background, func(ctx Context) {
		c1 := NewChannel(ctx)
		c2 = NewChannel(ctx)
		Go(ctx, func(ctx Context) {
			var v string
			more := c1.Receive(ctx, &v)
			assert.True(t, more)
			history = append(history, fmt.Sprintf("child1-end1-%v", v))
			more = c1.Receive(ctx, &v)
			assert.True(t, more)
			history = append(history, fmt.Sprintf("child1-end2-%v", v))
		})
		Go(ctx, func(ctx Context) {
			var v string
			history = append(history, "child2-start")
			more := c2.Receive(ctx, &v)
			assert.True(t, more)
			history = append(history, fmt.Sprintf("child2-end1-%v", v))
			more = c2.Receive(ctx, &v)
			assert.True(t, more)
			history = append(history, fmt.Sprintf("child2-end2-%v", v))
		})

		history = append(history, "root-before-channel-put")
		c1.Send(ctx, "value11")
		c1.Send(ctx, "value12")
		history = append(history, "root-after-channel-put")

	})
	require.EqualValues(t, 0, len(history))
	d.ExecuteUntilAllBlocked()
	c2.SendAsync("value21")
	d.ExecuteUntilAllBlocked()
	c2.SendAsync("value22")
	d.ExecuteUntilAllBlocked()

	require.True(t, d.IsDone(), d.StackTrace())
}

func TestBufferedChannelPut(t *testing.T) {
	var history []string
	d := newDispatcher(background, func(ctx Context) {
		c1 := NewBufferedChannel(ctx, 1)
		Go(ctx, func(ctx Context) {
			history = append(history, "child-start")
			var v1, v2 string
			more := c1.Receive(ctx, &v1)
			assert.True(t, more)
			history = append(history, fmt.Sprintf("child-end-%v", v1))
			c1.Receive(ctx, &v2)
			history = append(history, fmt.Sprintf("child-end-%v", v2))

		})
		history = append(history, "root-before-channel-put")
		c1.Send(ctx, "value1")
		history = append(history, "root-after-channel-put1")
		c1.Send(ctx, "value2")
		history = append(history, "root-after-channel-put2")
	})
	require.EqualValues(t, 0, len(history))
	d.ExecuteUntilAllBlocked()
	require.True(t, d.IsDone())

	expected := []string{
		"root-before-channel-put",
		"root-after-channel-put1",
		"child-start",
		"child-end-value1",
		"child-end-value2",
		"root-after-channel-put2",
	}
	require.EqualValues(t, expected, history)
}

func TestBufferedChannelGet(t *testing.T) {
	var history []string
	d := newDispatcher(background, func(ctx Context) {
		c1 := NewChannel(ctx)
		c2 := NewBufferedChannel(ctx, 2)

		Go(ctx, func(ctx Context) {
			history = append(history, "child1-start")
			c2.Send(ctx, "bar1")
			history = append(history, "child1-get")
			var v1 string
			more := c1.Receive(ctx, &v1)
			assert.True(t, more)
			history = append(history, fmt.Sprintf("child1-end-%v", v1))

		})
		Go(ctx, func(ctx Context) {
			history = append(history, "child2-start")
			c2.Send(ctx, "bar2")
			history = append(history, "child2-get")
			var v1 string
			more := c1.Receive(ctx, &v1)
			assert.True(t, more)
			history = append(history, fmt.Sprintf("child2-end-%v", v1))
		})
		history = append(history, "root-before-channel-get1")
		c2.Receive(ctx, nil)
		history = append(history, "root-before-channel-get2")
		c2.Receive(ctx, nil)
		history = append(history, "root-before-channel-put")
		c1.Send(ctx, "value1")
		history = append(history, "root-after-channel-put1")
		c1.Send(ctx, "value2")
		history = append(history, "root-after-channel-put2")
	})
	require.EqualValues(t, 0, len(history))
	d.ExecuteUntilAllBlocked()
	require.True(t, d.IsDone(), strings.Join(history, "\n")+"\n\n"+d.StackTrace())

	expected := []string{
		"root-before-channel-get1",
		"child1-start",
		"child1-get",
		"child2-start",
		"child2-get",
		"root-before-channel-get2",
		"root-before-channel-put",
		"root-after-channel-put1",
		"root-after-channel-put2",
		"child1-end-value1",
		"child2-end-value2",
	}
	require.EqualValues(t, expected, history)
}

func TestNotBlockingSelect(t *testing.T) {
	var history []string
	d := newDispatcher(background, func(ctx Context) {
		c1 := NewBufferedChannel(ctx, 1)
		c2 := NewBufferedChannel(ctx, 1)
		s := NewSelector(ctx)
		s.
			AddReceive(c1, func(c Channel, more bool) {
				assert.True(t, more)
				var v string
				c.Receive(ctx, &v)
				history = append(history, fmt.Sprintf("c1-%v", v))
			}).
			AddReceive(c2, func(c Channel, more bool) {
				assert.True(t, more)
				var v string
				c.Receive(ctx, &v)
				history = append(history, fmt.Sprintf("c2-%v", v))
			}).
			AddDefault(func() { history = append(history, "default") })
		c1.Send(ctx, "one")
		s.Select(ctx)
		c2.Send(ctx, "two")
		s.Select(ctx)
		s.Select(ctx)
	})
	d.ExecuteUntilAllBlocked()
	require.True(t, d.IsDone())

	expected := []string{
		"c1-one",
		"c2-two",
		"default",
	}
	require.EqualValues(t, expected, history)
}

func TestBlockingSelect(t *testing.T) {
	var history []string
	d := newDispatcher(background, func(ctx Context) {
		c1 := NewChannel(ctx)
		c2 := NewChannel(ctx)
		Go(ctx, func(ctx Context) {
			history = append(history, "add-one")
			c1.Send(ctx, "one")
			history = append(history, "add-one-done")

		})
		Go(ctx, func(ctx Context) {
			history = append(history, "add-two")
			c2.Send(ctx, "two")
			history = append(history, "add-two-done")
		})

		s := NewSelector(ctx)
		s.
			AddReceive(c1, func(c Channel, more bool) {
				assert.True(t, more)
				var v string
				c.Receive(ctx, &v)
				history = append(history, fmt.Sprintf("c1-%v", v))
			}).
			AddReceive(c2, func(c Channel, more bool) {
				var v string
				c.Receive(ctx, &v)
				history = append(history, fmt.Sprintf("c2-%v", v))
			})
		history = append(history, "select1")
		s.Select(ctx)
		history = append(history, "select2")
		s.Select(ctx)
		history = append(history, "done")
	})
	d.ExecuteUntilAllBlocked()
	require.True(t, d.IsDone(), strings.Join(history, "\n"))

	expected := []string{
		"select1",
		"add-one",
		"add-one-done",
		"add-two",
		"c1-one",
		"select2",
		"c2-two",
		"done",
		"add-two-done",
	}
	require.EqualValues(t, expected, history)
}

func TestBlockingSelectAsyncSend(t *testing.T) {
	var history []string
	d := newDispatcher(background, func(ctx Context) {

		c1 := NewChannel(ctx)
		s := NewSelector(ctx)
		s.
			AddReceive(c1, func(c Channel, more bool) {
				assert.True(t, more)
				var v int
				c.Receive(ctx, &v)
				history = append(history, fmt.Sprintf("c1-%v", v))
			})
		for i := 0; i < 3; i++ {
			ii := i // to reference within closure
			Go(ctx, func(ctx Context) {
				history = append(history, fmt.Sprintf("add-%v", ii))
				c1.SendAsync(ii)
			})
			history = append(history, fmt.Sprintf("select-%v", ii))
			s.Select(ctx)
		}
		history = append(history, "done")
	})
	d.ExecuteUntilAllBlocked()
	require.True(t, d.IsDone(), strings.Join(history, "\n"))

	expected := []string{
		"select-0",
		"add-0",
		"c1-0",
		"select-1",
		"add-1",
		"c1-1",
		"select-2",
		"add-2",
		"c1-2",
		"done",
	}
	require.EqualValues(t, expected, history)
}

func TestSendSelect(t *testing.T) {
	var history []string
	d := newDispatcher(background, func(ctx Context) {
		c1 := NewChannel(ctx)
		c2 := NewChannel(ctx)
		Go(ctx, func(ctx Context) {
			history = append(history, "receiver")
			var v string
			more := c2.Receive(ctx, &v)
			assert.True(t, more)
			history = append(history, fmt.Sprintf("c2-%v", v))
			more = c1.Receive(ctx, &v)

			assert.True(t, more)
			history = append(history, fmt.Sprintf("c1-%v", v))
		})
		s := NewSelector(ctx)
		s.AddSend(c1, "one", func() { history = append(history, "send1") }).
			AddSend(c2, "two", func() { history = append(history, "send2") })
		history = append(history, "select1")
		s.Select(ctx)
		history = append(history, "select2")
		s.Select(ctx)
		history = append(history, "done")
	})
	d.ExecuteUntilAllBlocked()
	require.True(t, d.IsDone())

	expected := []string{
		"select1",
		"receiver",
		"c2-two",
		"send2",
		"select2",
		"send1",
		"done",
		"c1-one",
	}
	require.EqualValues(t, expected, history)
}

func TestSendSelectWithAsyncReceive(t *testing.T) {
	var history []string
	d := newDispatcher(background, func(ctx Context) {
		c1 := NewChannel(ctx)
		c2 := NewChannel(ctx)
		Go(ctx, func(ctx Context) {
			history = append(history, "receiver")
			var v string
			ok, more := c2.ReceiveAsyncWithMoreFlag(&v)
			assert.True(t, ok)
			assert.True(t, more)
			history = append(history, fmt.Sprintf("c2-%v", v))
			more = c1.Receive(ctx, &v)

			assert.True(t, more)
			history = append(history, fmt.Sprintf("c1-%v", v))
		})
		s := NewSelector(ctx)
		s.AddSend(c1, "one", func() { history = append(history, "send1") }).
			AddSend(c2, "two", func() { history = append(history, "send2") })
		history = append(history, "select1")
		s.Select(ctx)
		history = append(history, "select2")
		s.Select(ctx)
		history = append(history, "done")
	})
	d.ExecuteUntilAllBlocked()
	require.True(t, d.IsDone(), strings.Join(history, "\n"))

	expected := []string{
		"select1",
		"receiver",
		"c2-two",
		"send2",
		"select2",
		"send1",
		"done",
		"c1-one",
	}
	require.EqualValues(t, expected, history)
}

func TestChannelClose(t *testing.T) {
	var history []string
	d := newDispatcher(background, func(ctx Context) {
		jobs := NewBufferedChannel(ctx, 5)
		done := NewNamedChannel(ctx, "done")

		GoNamed(ctx, "receiver", func(ctx Context) {
			for {
				var j int
				more := jobs.Receive(ctx, &j)
				if more {
					history = append(history, fmt.Sprintf("received job %v", j))
				} else {
					history = append(history, "received all jobs")
					done.Send(ctx, true)
					return
				}
			}
		})
		for j := 1; j <= 3; j++ {
			jobs.Send(ctx, j)
			history = append(history, fmt.Sprintf("sent job %v", j))
		}
		jobs.Close()
		history = append(history, "sent all jobs")
		done.Receive(ctx, nil)
		history = append(history, "done")

	})
	require.EqualValues(t, 0, len(history))
	d.ExecuteUntilAllBlocked()
	require.True(t, d.IsDone(), d.StackTrace())

	expected := []string{
		"sent job 1",
		"sent job 2",
		"sent job 3",
		"sent all jobs",
		"received job 1",
		"received job 2",
		"received job 3",
		"received all jobs",
		"done",
	}
	require.EqualValues(t, expected, history)
}

func TestSendClosedChannel(t *testing.T) {
	d := newDispatcher(background, func(ctx Context) {
		defer func() {
			assert.NotNil(t, recover(), "panic expected")
		}()
		c := NewChannel(ctx)
		Go(ctx, func(ctx Context) {
			c.Close()
		})
		c.Send(ctx, "baz")
	})
	d.ExecuteUntilAllBlocked()
	require.True(t, d.IsDone())
}

func TestBlockedSendClosedChannel(t *testing.T) {
	d := newDispatcher(background, func(ctx Context) {
		defer func() {
			assert.NotNil(t, recover(), "panic expected")
		}()
		c := NewBufferedChannel(ctx, 5)
		c.Send(ctx, "bar")
		c.Close()
		c.Send(ctx, "baz")
	})
	d.ExecuteUntilAllBlocked()
	require.True(t, d.IsDone())
}

func TestAsyncSendClosedChannel(t *testing.T) {
	d := newDispatcher(background, func(ctx Context) {
		defer func() {
			assert.NotNil(t, recover(), "panic expected")
		}()
		c := NewBufferedChannel(ctx, 5)
		c.Send(ctx, "bar")
		c.Close()
		_ = c.SendAsync("baz")
	})
	d.ExecuteUntilAllBlocked()
	require.True(t, d.IsDone())
}

func TestDispatchClose(t *testing.T) {
	var history []string
	d := newDispatcher(background, func(ctx Context) {
		c := NewNamedChannel(ctx, "forever_blocked")
		for i := 0; i < 10; i++ {
			ii := i
			GoNamed(ctx, fmt.Sprintf("c-%v", i), func(ctx Context) {
				c.Receive(ctx, nil) // blocked forever
				history = append(history, fmt.Sprintf("child-%v", ii))
			})
		}
		history = append(history, "root")
		c.Receive(ctx, nil) // blocked forever
	})
	require.EqualValues(t, 0, len(history))
	d.ExecuteUntilAllBlocked()
	require.False(t, d.IsDone())
	stack := d.StackTrace()
	// 11 coroutines (3 lines each) + 10 nl
	require.EqualValues(t, 11*3+10, len(strings.Split(stack, "\n")), stack)
	require.Contains(t, stack, "coroutine 1 [blocked on forever_blocked.Receive]:")
	for i := 0; i < 10; i++ {
		require.Contains(t, stack, fmt.Sprintf("coroutine c-%v [blocked on forever_blocked.Receive]:", i))
	}
	beforeClose := runtime.NumGoroutine()
	d.Close()
	time.Sleep(100 * time.Millisecond) // Let all goroutines to die
	closedCount := beforeClose - runtime.NumGoroutine()
	require.EqualValues(t, 11, closedCount)
	expected := []string{
		"root",
	}
	require.EqualValues(t, expected, history)
}

func TestPanic(t *testing.T) {
	var history []string
	d := newDispatcher(background, func(ctx Context) {
		c := NewNamedChannel(ctx, "forever_blocked")
		for i := 0; i < 10; i++ {
			ii := i
			GoNamed(ctx, fmt.Sprintf("c-%v", i), func(ctx Context) {
				if ii == 9 {
					panic("simulated failure")
				}
				c.Receive(ctx, nil) // blocked forever
				history = append(history, fmt.Sprintf("child-%v", ii))
			})
		}
		history = append(history, "root")
		c.Receive(ctx, nil) // blocked forever
	})
	require.EqualValues(t, 0, len(history))
	err := d.ExecuteUntilAllBlocked()
	require.NotNil(t, err)
	var value string
	err.Value(&value)
	require.EqualValues(t, "simulated failure", value)
	require.EqualValues(t, "simulated failure", err.Error())

	require.Contains(t, err.StackTrace(), "cadence.TestPanic")
}

func TestFutureSetValue(t *testing.T) {
	var history []string
	var f Future
	var s Settable
	d := newDispatcher(background, func(ctx Context) {
		f, s = NewFuture(ctx)
		Go(ctx, func(ctx Context) {
			history = append(history, "child-start")
			assert.False(t, f.IsReady())
			var v string
			err := f.Get(ctx, &v)
			assert.Nil(t, err)
			assert.True(t, f.IsReady())
			history = append(history, fmt.Sprintf("future-get-%v", v))
			// test second get of the ready future
			err = f.Get(ctx, &v)
			assert.Nil(t, err)
			assert.True(t, f.IsReady())
			history = append(history, fmt.Sprintf("child-end-%v", v))
		})
		history = append(history, "root-end")

	})
	require.EqualValues(t, 0, len(history))
	err := d.ExecuteUntilAllBlocked()
	if err != nil {
		require.NoError(t, err, err.StackTrace())
	}
	require.False(t, d.IsDone(), fmt.Sprintf("%v", d.StackTrace()))
	history = append(history, "future-set")
	assert.False(t, f.IsReady())
	s.SetValue("value1")
	assert.True(t, f.IsReady())
	err = d.ExecuteUntilAllBlocked()
	if err != nil {
		require.NoError(t, err, err.StackTrace())
	}
	require.True(t, d.IsDone())

	expected := []string{
		"root-end",
		"child-start",
		"future-set",
		"future-get-value1",
		"child-end-value1",
	}
	require.EqualValues(t, expected, history)

}

func TestFutureFail(t *testing.T) {
	var history []string
	var f Future
	var s Settable
	d := newDispatcher(background, func(ctx Context) {
		f, s = NewFuture(ctx)
		Go(ctx, func(ctx Context) {
			history = append(history, "child-start")
			assert.False(t, f.IsReady())
			var v string
			err := f.Get(ctx, &v)
			assert.NotNil(t, err)
			assert.True(t, f.IsReady())
			history = append(history, fmt.Sprintf("future-get-%v", err))
			// test second get of the ready future
			err = f.Get(ctx, &v)
			assert.NotNil(t, err)
			assert.True(t, f.IsReady())
			history = append(history, fmt.Sprintf("child-end-%v", err))
		})
		history = append(history, "root-end")

	})
	require.EqualValues(t, 0, len(history))
	err := d.ExecuteUntilAllBlocked()
	if err != nil {
		require.NoError(t, err, err.StackTrace())
	}
	require.False(t, d.IsDone(), fmt.Sprintf("%v", d.StackTrace()))
	history = append(history, "future-set")
	assert.False(t, f.IsReady())
	s.SetError(errors.New("value1"))
	assert.True(t, f.IsReady())
	err = d.ExecuteUntilAllBlocked()
	if err != nil {
		require.NoError(t, err, err.StackTrace())
	}
	require.True(t, d.IsDone())

	expected := []string{
		"root-end",
		"child-start",
		"future-set",
		"future-get-value1",
		"child-end-value1",
	}
	require.EqualValues(t, expected, history)
}

func TestFutureSet(t *testing.T) {
	var history []string
	var f Future
	var s Settable
	d := newDispatcher(background, func(ctx Context) {
		f, s = NewFuture(ctx)
		Go(ctx, func(ctx Context) {
			history = append(history, "child-start")
			assert.False(t, f.IsReady())
			var v string
			err := f.Get(ctx, &v)
			assert.NotNil(t, err)
			assert.NotNil(t, v)
			assert.True(t, f.IsReady())
			history = append(history, fmt.Sprintf("future-get-%v-%v", v, err))
			// test second get of the ready future
			err = f.Get(ctx, &v)
			assert.NotNil(t, err)
			assert.NotNil(t, v)
			assert.True(t, f.IsReady())
			history = append(history, fmt.Sprintf("child-end-%v-%v", v, err))
		})
		history = append(history, "root-end")

	})
	require.EqualValues(t, 0, len(history))
	err := d.ExecuteUntilAllBlocked()
	if err != nil {
		require.NoError(t, err, err.StackTrace())
	}
	require.False(t, d.IsDone(), fmt.Sprintf("%v", d.StackTrace()))
	history = append(history, "future-set")
	assert.False(t, f.IsReady())
	s.Set("value1", errors.New("error1"))
	assert.True(t, f.IsReady())
	err = d.ExecuteUntilAllBlocked()
	if err != nil {
		require.NoError(t, err, err.StackTrace())
	}
	require.True(t, d.IsDone())

	expected := []string{
		"root-end",
		"child-start",
		"future-set",
		"future-get-value1-error1",
		"child-end-value1-error1",
	}
	require.EqualValues(t, expected, history)
}

func TestFutureChain(t *testing.T) {
	var history []string
	var f, cf Future
	var s, cs Settable

	d := newDispatcher(background, func(ctx Context) {
		f, s = NewFuture(ctx)
		cf, cs = NewFuture(ctx)
		s.Chain(cf)
		Go(ctx, func(ctx Context) {
			history = append(history, "child-start")
			assert.False(t, f.IsReady())
			var v string
			err := f.Get(ctx, &v)
			assert.NotNil(t, err)
			assert.NotNil(t, v)
			assert.True(t, f.IsReady())
			history = append(history, fmt.Sprintf("future-get-%v-%v", v, err))
			// test second get of the ready future
			err = f.Get(ctx, &v)
			assert.NotNil(t, err)
			assert.NotNil(t, v)
			assert.True(t, f.IsReady())
			history = append(history, fmt.Sprintf("child-end-%v-%v", v, err))
		})
		history = append(history, "root-end")

	})
	require.EqualValues(t, 0, len(history))
	err := d.ExecuteUntilAllBlocked()
	if err != nil {
		require.NoError(t, err, err.StackTrace())
	}
	require.False(t, d.IsDone(), fmt.Sprintf("%v", d.StackTrace()))
	history = append(history, "future-set")
	assert.False(t, f.IsReady())
	cs.Set("value1", errors.New("error1"))
	assert.True(t, f.IsReady())
	err = d.ExecuteUntilAllBlocked()
	if err != nil {
		require.NoError(t, err, err.StackTrace())
	}
	require.True(t, d.IsDone())

	expected := []string{
		"root-end",
		"child-start",
		"future-set",
		"future-get-value1-error1",
		"child-end-value1-error1",
	}
	require.EqualValues(t, expected, history)
}

func TestSelectFuture(t *testing.T) {
	var history []string
	d := newDispatcher(background, func(ctx Context) {
		future1, settable1 := NewFuture(ctx)
		future2, settable2 := NewFuture(ctx)
		Go(ctx, func(ctx Context) {
			history = append(history, "add-one")
			settable1.SetValue("one")
		})
		Go(ctx, func(ctx Context) {
			history = append(history, "add-two")
			settable2.SetValue("two")
		})

		s := NewSelector(ctx)
		s.
			AddFuture(future1, func(f Future) {
				var v string
				err := f.Get(ctx, &v)
				assert.Nil(t, err)
				history = append(history, fmt.Sprintf("c1-%v", v))
			}).
			AddFuture(future2, func(f Future) {
				var v string
				err := f.Get(ctx, &v)
				assert.Nil(t, err)
				history = append(history, fmt.Sprintf("c2-%v", v))
			})
		history = append(history, "select1")
		s.Select(ctx)
		history = append(history, "select2")
		s.Select(ctx)
		history = append(history, "done")
	})
	err := d.ExecuteUntilAllBlocked()
	if err != nil {
		require.NoError(t, err, err.StackTrace())
	}
	require.True(t, d.IsDone())

	expected := []string{
		"select1",
		"add-one",
		"add-two",
		"c1-one",
		"select2",
		"c2-two",
		"done",
	}
	require.EqualValues(t, expected, history)
}

func TestSelectDecodeFuture(t *testing.T) {
	var history []string
	d := newDispatcher(background, func(ctx Context) {
		future1, settable1 := newDecodeFuture(ctx, "testFn1")
		future2, settable2 := newDecodeFuture(ctx, "testFn2")
		Go(ctx, func(ctx Context) {
			history = append(history, "add-one")
			settable1.SetValue([]byte("one"))
		})
		Go(ctx, func(ctx Context) {
			history = append(history, "add-two")
			settable2.SetValue([]byte("two"))
		})

		s := NewSelector(ctx)
		s.
			AddFuture(future1, func(f Future) {
				var v []byte
				err := f.Get(ctx, &v)
				assert.NoError(t, err)
				history = append(history, fmt.Sprintf("c1-%s", v))
			}).
			AddFuture(future2, func(f Future) {
				var v []byte
				err := f.Get(ctx, &v)
				assert.NoError(t, err)
				history = append(history, fmt.Sprintf("c2-%s", v))
			})
		history = append(history, "select1")
		s.Select(ctx)
		history = append(history, "select2")
		s.Select(ctx)
		history = append(history, "done")
	})
	err := d.ExecuteUntilAllBlocked()
	if err != nil {
		require.NoError(t, err, err.StackTrace())
	}
	require.True(t, d.IsDone())

	expected := []string{
		"select1",
		"add-one",
		"add-two",
		"c1-one",
		"select2",
		"c2-two",
		"done",
	}
	require.EqualValues(t, expected, history)
}

func TestDecodeFutureChain(t *testing.T) {
	var history []string
	var f, cf Future
	var s, cs Settable

	d := newDispatcher(background, func(ctx Context) {
		f, s = newDecodeFuture(ctx, "testFn")
		cf, cs = newDecodeFuture(ctx, "testFun")
		s.Chain(cf)
		Go(ctx, func(ctx Context) {
			history = append(history, "child-start")
			assert.False(t, f.IsReady())
			var v []byte
			err := f.Get(ctx, &v)
			assert.NotNil(t, err)
			assert.NotNil(t, v)
			assert.True(t, f.IsReady())
			history = append(history, fmt.Sprintf("future-get-%s-%v", v, err))
			// test second get of the ready future
			err = f.Get(ctx, &v)
			assert.NotNil(t, err)
			assert.NotNil(t, v)
			assert.True(t, f.IsReady())
			history = append(history, fmt.Sprintf("child-end-%s-%v", v, err))
		})
		history = append(history, "root-end")

	})
	require.EqualValues(t, 0, len(history))
	err := d.ExecuteUntilAllBlocked()
	if err != nil {
		require.NoError(t, err, err.StackTrace())
	}
	require.False(t, d.IsDone(), fmt.Sprintf("%v", d.StackTrace()))
	history = append(history, "future-set")
	assert.False(t, f.IsReady())
	cs.Set([]byte("value1"), errors.New("error1"))
	assert.True(t, f.IsReady())
	err = d.ExecuteUntilAllBlocked()
	if err != nil {
		require.NoError(t, err, err.StackTrace())
	}
	require.True(t, d.IsDone())

	expected := []string{
		"root-end",
		"child-start",
		"future-set",
		"future-get-value1-error1",
		"child-end-value1-error1",
	}
	require.EqualValues(t, expected, history)
}