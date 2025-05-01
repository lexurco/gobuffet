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

package item

import (
	"flag"
	"fmt"
	"os"

	iutil "gobuffet/item/util"
	"gobuffet/util"
)

var (
	flags  = flag.NewFlagSet(os.Args[0] + " item", flag.ExitOnError)
	dbFlag = flags.String("db", "",
		"database connection string or URI (environment is used if empty)")

	addFlags = flag.NewFlagSet("item add", flag.ExitOnError)
	descrAddFlag, imgAddFlag string
	idAddFlag int
	priceAddFlag iutil.Price = 0

	modFlags = flag.NewFlagSet(os.Args[0] + " item mod", flag.ExitOnError)
	nameModFlag, descrModFlag, imgModFlag string
	nodescrModFlag, noimgModFlag bool
	idModFlag int
	priceModFlag iutil.Price = -1
)

func init() {
	addFlags.StringVar(&descrAddFlag, "descr", "", "item description")
	addFlags.StringVar(&imgAddFlag, "img", "", "item image")
	addFlags.IntVar(&idAddFlag, "id", -1, "item id (automatic if <0)")
	addFlags.Var(&priceAddFlag, "price", "item price")

	modFlags.StringVar(&nameModFlag, "name", "", "new name")
	modFlags.StringVar(&descrModFlag, "descr", "", "new description")
	modFlags.StringVar(&imgModFlag, "img", "", "new image")
	modFlags.BoolVar(&nodescrModFlag, "nodescr", false, "remove any description")
	modFlags.BoolVar(&noimgModFlag, "noimg", false, "remove any image")
	modFlags.IntVar(&idModFlag, "id", -1, "new id (ignored if <0)")
	modFlags.Var(&priceModFlag, "price", "new price")
}

func cmdAdd(args []string) {
	var err     error
	var it      iutil.Item
	var imgFile *os.File

	addFlags.Parse(args[1:])
	args = addFlags.Args()
	switch len(args) {
	case 1:
		if args[0] == "" {
			util.Die("name cannot be empty")
		}
		it.Name = &args[0]
	case 0:
		fallthrough
	default:
		util.Die("no name specified")
	}

	if idAddFlag >= 0 {
		it.ID = &idAddFlag
	}
	if descrAddFlag != "" {
		it.Descr = &descrAddFlag
	}
	if imgAddFlag != "" {
		it.Img.Name = &imgAddFlag
		imgFile, err = os.Open(imgAddFlag)
		it.Img.Reader = imgFile
		if err != nil {
			util.Die(err)
		}
		defer imgFile.Close()
	}

	it.Price = (*int)(&priceAddFlag)

	db, err := util.DBConnect(*dbFlag)
	if err != nil {
		util.Die(err)
	}
	defer db.Close()

	if err = iutil.Add(db, &it); err != nil {
		util.Die(err)
	}
}

func cmdDel(args []string) {
	var names []string
	var ids []int

	for _, a := range args[1:] {
		id, name, err := iutil.ParseItem(a)
		if err != nil {
			util.Die(err)
		}
		if id >= 0 {
			ids = append(ids, id)
		} else {
			names = append(names, name)
		}
	}

	db, err := util.DBConnect(*dbFlag)
	if err != nil {
		util.Die(err)
	}
	defer db.Close()

	if err := iutil.Del(db, ids, names); err != nil {
		util.Die(err)
	}
}

func cmdMod(args []string) {
	var it      iutil.Item
	var err     error
	var imgFile *os.File

	modFlags.Parse(args[1:])
	args = modFlags.Args()
	if len(args) != 1 {
		util.Die("usage: "+os.Args[0]+" item mod [flags ...] item")
	}

	if nameModFlag != "" {
		it.Name = &nameModFlag
	}

	if idModFlag >= 0 {
		it.ID = &idModFlag
	}

	if nodescrModFlag {
		descrModFlag = ""
		it.Descr = &descrModFlag
	} else if descrModFlag != "" {
		it.Descr = &descrModFlag
	}

	if priceModFlag >= 0 {
		it.Price = (*int)(&priceModFlag)
	}

	if noimgModFlag {
		imgModFlag = ""
		it.Img.Name = &imgModFlag
	} else if imgModFlag != "" {
		if imgFile, err = os.Open(imgModFlag); err != nil {
			util.Die(err)
		}
		defer imgFile.Close()
		it.Img.Reader = imgFile
		it.Img.Name = &imgModFlag
	}

	id, name, err := iutil.ParseItem(args[0])
	if err != nil {
		util.Die(err)
	}

	db, err := util.DBConnect(*dbFlag)
	if err != nil {
		util.Die(err)
	}
	defer db.Close()

	iutil.Mod(db, id, name, &it)
}

func cmdShow(args []string) {
	var names []string
	var ids []int

	for _, a := range args[1:] {
		id, name, err := iutil.ParseItem(a)
		if err != nil {
			util.Die(err)
		}
		if id >= 0 {
			ids = append(ids, id)
		} else {
			names = append(names, name)
		}
	}

	db, err := util.DBConnect(*dbFlag)
	if err != nil {
		util.Die(err)
	}
	defer db.Close()

	items, err := iutil.Get(db, ids, names, iutil.ByID)
	if err != nil {
		util.Die(err)
	}
	fmt.Printf("%5v %15v %8v %40v %v\n", "ID", "NAME", "PRICE", "IMAGE", "DESCRIPTION")
	for i := range items {
		var descr, img string

		if items[i].Descr != nil {
			descr = *items[i].Descr
		} else {
			descr = "-"
		}
		if items[i].Img.Name != nil {
			img = *items[i].Img.Name
		} else {
			img = "-"
		}

		fmt.Printf("%5v %15v %5v.%02v %40v %v\n", *items[i].ID, *items[i].Name,
			*items[i].Price/100, *items[i].Price%100, img, descr)
	}
}

func Item(args []string) {
	flags.Parse(args[1:])
	if args = flags.Args(); len(args) < 1 {
		util.Die("usage: "+os.Args[0]+" item [flags ...] command")
	}

	switch args[0] {
	case "add":
		cmdAdd(args)
	case "del":
		cmdDel(args)
	case "mod":
		cmdMod(args)
	case "show":
		cmdShow(args)
	default:
		util.Die("unknown subcommand: " + args[0] + "\n" +
			"available subcommands: add, del, mod, show")
	}
}
