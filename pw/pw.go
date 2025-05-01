// COPYRIGHT (c) 2025 Eneik
//
// Permission to use, copy, modify, and distribute this software for any
// purpose with or without fee is hereby granted, provided that the above
// copyright notice and this permission notice appear in all copies.
//
// THE SOFTWARE IS PROVIDED "AS IS" AND THE AUTHOR DISCLAIMS ALL WARRANTIES
// WITH REGARD TO THIS SOFTWARE INCLUDING ALL IMPLIED WARRANTIES OF
// MERCHANTABILITY AND FITNESS. IN NO EVENT SHALL THE AUTHOR BE LIABLE FOR
// ANY SPECIAL, DIRECT, INDIRECT, OR CONSEQUENTIAL DAMAGES OR ANY DAMAGES
// WHATSOEVER RESULTING FROM LOSS OF USE, DATA OR PROFITS, WHETHER IN AN
// ACTION OF CONTRACT, NEGLIGENCE OR OTHER TORTIOUS ACTION, ARISING OUT OF
// OR IN CONNECTION WITH THE USE OR PERFORMANCE OF THIS SOFTWARE.

package pw

import (
	"errors"
	"fmt"
	"flag"
	"os"
	"syscall"

	"golang.org/x/term"

	putil "github.com/lexurco/gobuffet/pw/util"
	"github.com/lexurco/gobuffet/util"
)

var flags = flag.NewFlagSet("pw", flag.ExitOnError)
var dbFlag = flags.String("db", "", "database connection string or URI")

func pwGet() (pass []byte, err error) {
	if !term.IsTerminal(syscall.Stdin) {
		return []byte{0}, errors.New("not a terminal")
	}
	tty, err := os.OpenFile("/dev/tty", os.O_RDONLY, 0)
	if err != nil {
		return []byte{0}, err
	}
	defer tty.Close()

	fmt.Print("password:")
	pass, err = term.ReadPassword(int(tty.Fd()))
	fmt.Println()
	return pass, err
}

func Pw(args []string) {
	var pass []byte
	var err error

	flags.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: rockbuffet pw [-db connstr] [password]")
		flags.PrintDefaults()
	}
	flags.Parse(args[1:])
	args = flags.Args()

	switch len(args) {
	case 0:
		// empty
	case 1:
		pass = []byte(args[0])
	default:
		flags.Usage()
	}

	db, err := util.DBConnect(*dbFlag)
	if err != nil {
		util.Die(err)
	}
	defer db.Close()

	if len(pass) == 0 {
		if pass, err = pwGet(); err != nil {
			util.Die(err)
		}
	}
	if err := putil.Chpass(db, pass); err != nil {
		util.Die(err)
	}
}
