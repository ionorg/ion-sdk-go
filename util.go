package engine

import (
	"math/rand"
	"os"
	"time"
)

func GetArgs(args ...string) (arg1, arg2, arg3, arg4, arg5, arg6, arg7, arg8, arg9, arg10 string) {
	// at least one
	if len(args) < 1 {
		return "", "", "", "", "", "", "", "", "", ""
	}
	// parse args
	for i, arg := range args {
		switch i {
		case 0:
			arg1 = arg
		case 1:
			arg2 = arg
		case 2:
			arg3 = arg
		case 3:
			arg4 = arg
		case 4:
			arg5 = arg
		case 5:
			arg6 = arg
		case 6:
			arg7 = arg
		case 7:
			arg8 = arg
		case 8:
			arg9 = arg
		case 9:
			arg10 = arg
		default:

		}
	}
	return arg1, arg2, arg3, arg4, arg5, arg6, arg7, arg8, arg9, arg10
}

// RandomKey generate a random string key
func RandomKey(n int) string {
	var letterRunes = []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ1234567890")
	rand.Seed(time.Now().UnixNano())
	b := make([]rune, n)
	for i := range b {
		b[i] = letterRunes[rand.Intn(len(letterRunes))]
	}
	return string(b)
}

func FileExist(path string) bool {
	_, err := os.Lstat(path)
	return !os.IsNotExist(err)
}
