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

package util

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"os"
	"path"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx"
)

func Die(a ...any) {
	fmt.Fprintln(os.Stderr, a...)
	os.Exit(1)
}

func ImgPath(base string) (path string) {
	return "img/" + base
}

func DBTest(conn *pgx.Conn) (err error) {
	if conn == nil {
		return errors.New("conn is nil")
	}
	for i := 0; i < 3; i++ {
		if err = conn.Ping(context.Background()); err == nil {
			return nil
		}
		time.Sleep(time.Second)
	}
	return err
}

func DBConnect(s string) (conn *pgx.Conn, err error) {
	// set default connection parameters as in libpq
	if os.Getenv("PGHOST") == "" && runtime.GOOS != "windows" {
		os.Setenv("PGHOST", "/tmp")
	}

	var conf pgx.ConnConfig
	if s == "" {
		conf, err = pgx.ParseEnvLibpq()
	} else {
		conf, err = pgx.ParseConnectionString(s)
	}
	if err != nil {
		return nil, err
	}

	conn, err = pgx.Connect(conf)
	if err != nil {
		return nil, err
	}

	return conn, nil
}

type Item struct {
	Name  *string
	Descr *string
	Img   struct {
		Name string
		File multipart.File
	}
	Price *int
}

func ItemAdd(db *pgx.Conn, i *Item) (err error) {
	var img, imgPath string
	cols := []string{"name", "price"}
	vals := []string{"$1", "$2"}
	args := []interface{}{*i.Name, *i.Price}

	if i.Img.File != nil {
		img = time.Now().Format("20060102_150405") + "_" + path.Base(i.Img.Name)
		imgPath = ImgPath(img)

		err = func() (err error) {
			w, err := os.Create(imgPath)
			if err != nil {
				return err
			}
			defer w.Close()
			if _, err = io.Copy(w, i.Img.File); err != nil {
				os.Remove(imgPath)
				return err
			}
			return nil
		}()
		if err != nil {
			return err
		}

		cols = append(cols, "img")
		vals = append(vals, fmt.Sprintf("$%v", len(cols)))
		args = append(args, img)
	}
	if i.Descr != nil {
		cols = append(cols, "descr")
		vals = append(vals, fmt.Sprintf("$%v", len(cols)))
		args = append(args, i.Descr)
	}
	_, err = db.Exec(fmt.Sprintf("INSERT INTO items (%v) VALUES (%v)",
		strings.Join(cols, ","), strings.Join(vals, ",")), args...)
	if err != nil {
		if img != "" {
			os.Remove(imgPath) // XXX log error here
		}
		return err
	}

	return nil
}

func ItemMod(db *pgx.Conn, item string, i *Item) (err error) {
	fld := "name"
	var (
		cond interface{} = item
		sets []string
		args []interface{}
		img, imgPath string
	)

	id, err := strconv.Atoi(item)
	if err == nil {
		fld = "id"
		cond = id
	} else if err.(*strconv.NumError).Err != strconv.ErrSyntax {
		return err
	}

	if i.Img.File != nil {
		img = time.Now().Format("20060102_150405") + "_" + path.Base(i.Img.Name)
		imgPath = ImgPath(img)

		err = func() (err error) {
			w, err := os.Create(imgPath)
			if err != nil {
				return err
			}
			defer w.Close()
			if _, err = io.Copy(w, i.Img.File); err != nil {
				os.Remove(imgPath)
				return err
			}
			return nil
		}()
		if err != nil {
			return err
		}

		sets = append(sets, fmt.Sprintf("img = $%v", len(sets) + 1))
		args = append(args, img)
	}

	if i.Name != nil {
		sets = append(sets, fmt.Sprintf("name = $%v", len(sets) + 1))
		args = append(args, *i.Name)
	}
	if i.Descr != nil {
		sets = append(sets, fmt.Sprintf("descr = $%v", len(sets) + 1))
		args = append(args, *i.Descr)
	}
	if i.Price != nil {
		sets = append(sets, fmt.Sprintf("price = $%v", len(sets) + 1))
		args = append(args, *i.Price)
	}

	if len(sets) == 0 {
		return nil
	}
	args = append(args, cond)
	_, err = db.Exec(fmt.Sprintf("UPDATE items SET %v WHERE %v = $%v",
		strings.Join(sets, ","), fld, len(sets) + 1), args...)
	if err != nil {
		if img != "" {
			os.Remove(imgPath)
		}
		return err
	}

	return nil
}

func ItemDel(db *pgx.Conn, item string, useName bool) (err error) {
	fld := "name"
	var (
		arg interface{} = item
		img *string
	)

	if !useName {
		id, err := strconv.Atoi(item)
		if err == nil {
			fld = "id"
			arg = id
		} else if err.(*strconv.NumError).Err != strconv.ErrSyntax {
			return err
		}
	}

	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if err = tx.QueryRow("SELECT img FROM items WHERE "+fld+" = $1", arg).
		Scan(&img); err != nil && err != pgx.ErrNoRows {

		return err
	}
	_, err = tx.Exec("DELETE FROM items WHERE "+fld+" = $1", arg)
	if err != nil {
		return err
	}
	tx.Commit()

	if img != nil {
		os.Remove(ImgPath(*img))
	}

	return nil
}

func ItemLs(db *pgx.Conn) (err error) {
	rows, err := db.Query("SELECT id, name, descr, price, img FROM items")
	if err != nil && err != pgx.ErrNoRows {
		return err
	}

	for rows.Next() {
		var id, price int
		var name, descr, img string
		var descrPtr, imgPtr *string
		if err := rows.Scan(&id, &name, &descrPtr, &price, &imgPtr); err != nil {
			return err
		}
		if descrPtr != nil {
			descr = *descrPtr
		}
		if imgPtr != nil {
			img = *imgPtr
		}
		fmt.Println(id, name, descr, price, img)
	}
	return nil
}
