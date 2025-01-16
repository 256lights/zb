// Copyright 2024 The zb Authors
// SPDX-License-Identifier: MIT

package lua

import (
	"fmt"
	"math"
	"math/rand/v2"
	"reflect"
	"time"

	"zb.256lights.llc/pkg/internal/luacode"
)

// MathLibraryName is the conventional identifier for the [math library].
//
// [math library]: https://www.lua.org/manual/5.4/manual.html#6.7
const MathLibraryName = "math"

// NewOpenMath returns a [Function] that loads the standard math library.
// If a [RandomSource] is provided,
// then it is used for random number generation.
//
// The resulting function is intended to be used as an argument to [Require].
func NewOpenMath(src RandomSource) Function {
	return func(l *State) (int, error) {
		src := src
		if src == nil {
			src = new(pcgRandomSource)
			src.Seed(weakSeed(l))
		}

		err := NewLib(l, map[string]Function{
			"abs":       mathAbs,
			"acos":      mathAcos,
			"asin":      mathAsin,
			"atan":      mathAtan,
			"ceil":      mathCeil,
			"cos":       mathCos,
			"deg":       mathDeg,
			"exp":       mathExp,
			"tointeger": mathToInteger,
			"floor":     mathFloor,
			"fmod":      mathFmod,
			"ult":       mathULT,
			"log":       mathLog,
			"max":       mathMax,
			"min":       mathMin,
			"modf":      mathModf,
			"rad":       mathRad,
			"sin":       mathSin,
			"sqrt":      mathSqrt,
			"tan":       mathTan,
			"type":      mathType,

			"random":     nil,
			"randomseed": nil,
			"pi":         nil,
			"huge":       nil,
			"maxinteger": nil,
			"mininteger": nil,
		})
		if err != nil {
			return 0, err
		}

		l.PushNumber(math.Pi)
		l.RawSetField(-2, "pi")
		l.PushNumber(math.Inf(1))
		l.RawSetField(-2, "huge")
		l.PushInteger(math.MaxInt64)
		l.RawSetField(-2, "maxinteger")
		l.PushInteger(math.MinInt64)
		l.RawSetField(-2, "mininteger")

		l.PushClosure(0, func(l *State) (int, error) {
			return mathRandom(l, src)
		})
		l.RawSetField(-2, "random")
		l.PushClosure(0, func(l *State) (int, error) {
			return mathRandomSeed(l, src)
		})
		l.RawSetField(-2, "randomseed")

		return 1, nil
	}
}

func mathAbs(l *State) (int, error) {
	if l.IsInteger(1) {
		n, _ := l.ToInteger(1)
		if n < 0 {
			n = -n
		}
		l.PushInteger(n)
	} else {
		n, err := CheckNumber(l, 1)
		if err != nil {
			return 0, err
		}
		l.PushNumber(math.Abs(n))
	}
	return 1, nil
}

func mathSin(l *State) (int, error) {
	x, err := CheckNumber(l, 1)
	if err != nil {
		return 0, err
	}
	l.PushNumber(math.Sin(x))
	return 1, nil
}

func mathCos(l *State) (int, error) {
	x, err := CheckNumber(l, 1)
	if err != nil {
		return 0, err
	}
	l.PushNumber(math.Cos(x))
	return 1, nil
}

func mathTan(l *State) (int, error) {
	x, err := CheckNumber(l, 1)
	if err != nil {
		return 0, err
	}
	l.PushNumber(math.Tan(x))
	return 1, nil
}

func mathAsin(l *State) (int, error) {
	x, err := CheckNumber(l, 1)
	if err != nil {
		return 0, err
	}
	l.PushNumber(math.Asin(x))
	return 1, nil
}

func mathAcos(l *State) (int, error) {
	x, err := CheckNumber(l, 1)
	if err != nil {
		return 0, err
	}
	l.PushNumber(math.Acos(x))
	return 1, nil
}

func mathAtan(l *State) (int, error) {
	y, err := CheckNumber(l, 1)
	if err != nil {
		return 0, err
	}
	x := float64(1)
	if !l.IsNoneOrNil(2) {
		x, err = CheckNumber(l, 2)
		if err != nil {
			return 0, err
		}
	}
	l.PushNumber(math.Atan2(y, x))
	return 1, nil
}

func mathToInteger(l *State) (int, error) {
	n, ok := l.ToInteger(1)
	if !ok {
		if l.IsNone(1) {
			return 0, NewArgError(l, 1, "value expected")
		}
		l.PushNil()
		return 1, nil
	}
	l.PushInteger(n)
	return 1, nil
}

func mathFloor(l *State) (int, error) {
	if l.IsInteger(1) {
		l.SetTop(1)
		return 1, nil
	}

	d, err := CheckNumber(l, 1)
	if err != nil {
		return 0, err
	}
	if i, ok := luacode.FloatToInteger(d, luacode.Floor); ok {
		l.PushInteger(i)
	} else {
		l.PushNumber(math.Floor(d))
	}
	return 1, nil
}

func mathCeil(l *State) (int, error) {
	if l.IsInteger(1) {
		l.SetTop(1)
		return 1, nil
	}

	d, err := CheckNumber(l, 1)
	if err != nil {
		return 0, err
	}
	if i, ok := luacode.FloatToInteger(d, luacode.Ceil); ok {
		l.PushInteger(i)
	} else {
		l.PushNumber(math.Ceil(d))
	}
	return 1, nil
}

func mathFmod(l *State) (int, error) {
	if l.IsInteger(1) && l.IsInteger(2) {
		y, _ := l.ToInteger(2)
		switch y {
		case 0:
			return 0, NewArgError(l, 2, "zero")
		case -1:
			// Avoid overflow with 0x80000...
			l.PushInteger(0)
		default:
			x, _ := l.ToInteger(1)
			l.PushInteger(x % y)
		}
	} else {
		x, err := CheckNumber(l, 1)
		if err != nil {
			return 0, err
		}
		y, err := CheckNumber(l, 2)
		if err != nil {
			return 0, err
		}
		l.PushNumber(math.Mod(x, y))
	}
	return 1, nil
}

func mathModf(l *State) (int, error) {
	if l.IsInteger(1) {
		l.SetTop(1)
		l.PushNumber(0) // No fractional part.
		return 2, nil
	}

	n, err := CheckNumber(l, 1)
	if err != nil {
		return 0, err
	}
	// Round towards zero.
	var integerPart float64
	if n < 0 {
		integerPart = math.Ceil(n)
	} else {
		integerPart = math.Floor(n)
	}

	if i, ok := luacode.FloatToInteger(integerPart, luacode.OnlyIntegral); ok {
		l.PushInteger(i)
	} else {
		l.PushNumber(integerPart)
	}
	if n == integerPart {
		l.PushNumber(0)
	} else {
		l.PushNumber(n - integerPart)
	}
	return 2, nil
}

func mathSqrt(l *State) (int, error) {
	x, err := CheckNumber(l, 1)
	if err != nil {
		return 0, err
	}
	l.PushNumber(math.Sqrt(x))
	return 1, nil
}

func mathULT(l *State) (int, error) {
	x, err := CheckInteger(l, 1)
	if err != nil {
		return 0, err
	}
	y, err := CheckInteger(l, 2)
	if err != nil {
		return 0, err
	}
	l.PushBoolean(uint64(x) < uint64(y))
	return 1, nil
}

func mathLog(l *State) (int, error) {
	x, err := CheckNumber(l, 1)
	if err != nil {
		return 0, err
	}
	if l.IsNoneOrNil(2) {
		l.PushNumber(math.Log(x))
		return 1, nil
	}
	base, err := CheckNumber(l, 2)
	if err != nil {
		return 0, err
	}
	switch base {
	case 1:
		l.PushNumber(math.NaN())
	case 2:
		l.PushNumber(math.Log2(x))
	case 10:
		l.PushNumber(math.Log10(x))
	default:
		l.PushNumber(math.Log(x) / math.Log(base))
	}
	return 1, nil
}

func mathExp(l *State) (int, error) {
	x, err := CheckNumber(l, 1)
	if err != nil {
		return 0, err
	}
	l.PushNumber(math.Exp(x))
	return 1, nil
}

func mathDeg(l *State) (int, error) {
	x, err := CheckNumber(l, 1)
	if err != nil {
		return 0, err
	}
	l.PushNumber(x * (180 / math.Pi))
	return 1, nil
}

func mathRad(l *State) (int, error) {
	x, err := CheckNumber(l, 1)
	if err != nil {
		return 0, err
	}
	l.PushNumber(x * (math.Pi / 180))
	return 1, nil
}

func mathMin(l *State) (int, error) {
	n := l.Top()
	if n < 1 {
		return 0, NewArgError(l, 1, "value expected")
	}
	iMin := 1
	for i := 2; i <= n; i++ {
		isLess, err := l.Compare(i, iMin, Less, 0)
		if err != nil {
			return 0, err
		}
		if isLess {
			iMin = i
		}
	}
	l.PushValue(iMin)
	return 1, nil
}

func mathMax(l *State) (int, error) {
	n := l.Top()
	if n < 1 {
		return 0, NewArgError(l, 1, "value expected")
	}
	iMax := 1
	for i := 2; i <= n; i++ {
		isGreater, err := l.Compare(iMax, i, Less, 0)
		if err != nil {
			return 0, err
		}
		if isGreater {
			iMax = i
		}
	}
	l.PushValue(iMax)
	return 1, nil
}

func mathType(l *State) (int, error) {
	switch l.Type(1) {
	case TypeNumber:
		if l.IsInteger(1) {
			l.PushString("integer")
		} else {
			l.PushString("float")
		}
		return 1, nil
	case TypeNone:
		return 0, NewArgError(l, 1, "value expected")
	default:
		l.PushNil()
		return 1, nil
	}
}

// A RandomSource is a source of uniformly distributed pseudo-random uint64 values
// in the range [0, 1<<64).
// Calling Seed should reinitialize the pseudo-random number generator
// and return the seed used
// such that passing the returned seed should produce the same sequence of values.
//
// A RandomSource is not safe for concurrent use by multiple goroutines.
type RandomSource interface {
	rand.Source
	Seed(seed RandomSeed) (used RandomSeed)
}

type pcgRandomSource struct {
	rand.PCG
}

func (p *pcgRandomSource) Seed(seed RandomSeed) RandomSeed {
	p.PCG.Seed(uint64(seed[0]), uint64(seed[1]))
	return seed
}

func mathRandom(l *State, src RandomSource) (int, error) {
	r := rand.New(src)
	var lowerLimit, upperLimit int64
	switch l.Top() {
	case 0:
		l.PushNumber(r.Float64())
		return 1, nil
	case 1:
		lowerLimit = 1
		var err error
		upperLimit, err = CheckInteger(l, 1)
		if err != nil {
			return 0, err
		}
		if upperLimit == 0 {
			// "The call math.random(0) produces an integer with all bits (pseudo)random."
			l.PushInteger(int64(r.Uint64()))
			return 1, nil
		}
	case 2:
		var err error
		lowerLimit, err = CheckInteger(l, 1)
		if err != nil {
			return 0, err
		}
		upperLimit, err = CheckInteger(l, 2)
		if err != nil {
			return 0, err
		}
	default:
		return 0, fmt.Errorf("%swrong number of arguments", Where(l, 1))
	}

	if lowerLimit > upperLimit {
		return 0, NewArgError(l, 1, "interval is empty")
	}
	if lowerLimit == math.MinInt64 && upperLimit == math.MaxInt64 {
		i := r.Uint64()
		l.PushInteger(int64(i))
	} else {
		i := r.Uint64N(uint64(upperLimit) - uint64(lowerLimit) + 1)
		l.PushInteger(int64(uint64(lowerLimit) + i))
	}
	return 1, nil
}

// RandomSeed is a 128-bit value used to initialize a [RandomSource].
type RandomSeed [2]int64

// weakSeed returns a new [RandomSeed] with limited entropy.
func weakSeed(l *State) RandomSeed {
	return RandomSeed{
		time.Now().UnixMicro(),
		int64(reflect.ValueOf(l).Pointer()),
	}
}

func mathRandomSeed(l *State, src RandomSource) (int, error) {
	var seed RandomSeed
	if l.IsNone(1) {
		seed = weakSeed(l)
	} else {
		var err error
		seed[0], err = CheckInteger(l, 1)
		if err != nil {
			return 0, err
		}
		if !l.IsNoneOrNil(2) {
			seed[1], err = CheckInteger(l, 2)
			if err != nil {
				return 0, err
			}
		}
	}

	used := src.Seed(seed)
	for _, x := range used {
		l.PushInteger(x)
	}
	return len(used), nil
}
