/*
Copyright © 2016 Henry Huang <hhh@rutcode.com>

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
GNU General Public License for more details.

You should have received a copy of the GNU General Public License
along with this program. If not, see <http://www.gnu.org/licenses/>.
*/

package filters

import (
	"sync"
	"time"

	"github.com/iTrellis/common/errors"
)

type filter struct {
	compareFuncs map[string]CompareFunc

	compareTimeout time.Duration

	sync.RWMutex
}

// New generate an new target filter
func New() FilterRepo {
	return &filter{
		compareFuncs:   map[string]CompareFunc{EqualFunctionName: CompareEqualsFunc},
		compareTimeout: 30 * time.Second,
	}
}

type filterMessage struct {
	Name     string
	Filtered bool
	Err      error
}

// AddCompareFunc add filter functions
func (p *filter) AddCompareFunc(name string, cf CompareFunc) {
	p.Lock()
	defer p.Unlock()

	if name != "" && cf != nil {
		p.compareFuncs[name] = cf
	}
}

// RemoveCompareFunc remove a filter function
func (p *filter) RemoveCompareFunc(name string) {
	p.Lock()
	defer p.Unlock()

	if name != "" {
		delete(p.compareFuncs, name)
	}
}

// GetCompareFunc get a filter funcation by name
func (p *filter) GetCompareFunc(name string) CompareFunc {
	p.RLock()
	defer p.RUnlock()
	return p.compareFuncs[name]
}

// SetCompareTimeout set filter function timeout
func (p *filter) SetCompareTimeout(timeout time.Duration) error {
	p.Lock()
	defer p.Unlock()
	if timeout <= 0 {
		return ErrTimeoutMustAboveZero.New()
	}
	p.compareTimeout = timeout
	return nil
}

// Compare
func (p *filter) Compare(params *FilterParams, intput, target FilterValues) (filtered bool, err error) {

	if err = params.valid(); err != nil {
		return
	}

	fChan := make(chan filterMessage)

	// sequence to compare filter functions
	switch params.Type {
	case CompareTypeSequence:
		defer close(fChan)

		go func() {
			for _, name := range params.Names {
				fMessage := p.dofilter(name, intput, target)

				if fMessage.Err != nil || fMessage.Filtered {
					fChan <- fMessage
					return
				}
			}
			fChan <- filterMessage{}
		}()
	case CompareTypeConsistent:
		// consistent to compare filter functions
		var wg sync.WaitGroup
		wg.Add(len(params.Names))
		for _, name := range params.Names {
			go func(n string) {
				defer wg.Done()
				fChan <- p.dofilter(n, intput, target)
			}(name)
		}

		go func() {
			wg.Wait()
			close(fChan)
		}()
	default:
		return false, ErrUnknownCompareType.New()
	}

	for i := 0; i < len(params.Names); i++ {
		select {
		case filter := <-fChan:
			if filter.Err != nil {
				return filter.Filtered, ErrFailedExecFilterFunction.New(errors.Params{"err": err.Error()})
			} else if filter.Filtered {
				return filter.Filtered, nil
			}
		case <-time.After(p.compareTimeout):
			return true, ErrFailedExecTimeout.New()
		}
	}
	return
}

// dofilter execute filter function
func (p *filter) dofilter(name string, input, target FilterValues) filterMessage {
	fc := p.GetCompareFunc(name)
	if fc == nil {
		return filterMessage{Name: name}
	}

	f, e := fc(input, target)
	return filterMessage{Name: name, Filtered: f, Err: e}
}
