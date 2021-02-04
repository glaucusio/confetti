package codec

import (
	"archive/zip"
	"bytes"
	"io/ioutil"
	"path"
	"path/filepath"
	"strings"

	"github.com/rjeczalik/bigstruct/isr"
)

type Zip struct{}

var _ isr.Codec = (*Zip)(nil)

func (Zip) Encode(key string, o isr.Object) error {
	var (
		k = path.Base(key)
		n = o[k]
		f isr.Fields
	)

	n.Children.ForEach(f.Append)

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)

	for _, f := range f {
		p, err := tobytes(f.Value)
		if err != nil {
			return &isr.Error{
				Type: "zip",
				Op:   "encode",
				Key:  f.Key,
				Err:  err,
			}
		}

		if len(p) == 0 {
			continue // skip, nothing to encode
		}

		// make the path relative in the archive
		name := filepath.FromSlash(strings.TrimLeft(f.Key, `/\`))

		zf, err := zw.Create(name)
		if err != nil {
			return &isr.Error{
				Type: "zip",
				Op:   "encode",
				Key:  f.Key,
				Err:  err,
			}
		}

		if _, err := zf.Write(p); err != nil {
			return &isr.Error{
				Type: "zip",
				Op:   "encode",
				Key:  f.Key,
				Err:  err,
			}
		}
	}

	if err := zw.Close(); err != nil {
		return &isr.Error{
			Type: "zip",
			Op:   "encode",
			Key:  key,
			Err:  err,
		}
	}

	n.Value = buf.Bytes()
	n.Children = nil
	o[k] = n

	return nil
}

func (Zip) Decode(key string, o isr.Object) error {
	var (
		k = path.Base(key)
		n = o[k]
	)

	p, err := tobytes(n.Value)
	if err != nil {
		return &isr.Error{
			Type: "zip",
			Op:   "decode",
			Key:  key,
			Err:  err,
		}
	}

	zr, err := zip.NewReader(bytes.NewReader(p), int64(len(p)))
	if err != nil {
		return &isr.Error{
			Type: "zip",
			Op:   "decode",
			Key:  key,
			Err:  err,
		}
	}

	var f isr.Fields

	for _, zf := range zr.File {
		key := cleanpath(zf.Name)

		rc, err := zf.Open()
		if err != nil {
			return &isr.Error{
				Type: "zip",
				Op:   "decode",
				Key:  key,
				Err:  err,
			}
		}

		p, err := ioutil.ReadAll(rc)
		_ = rc.Close()
		if err != nil {
			return &isr.Error{
				Type: "zip",
				Op:   "decode",
				Key:  key,
				Err:  err,
			}
		}

		f = append(f, isr.Field{
			Key:   key,
			Value: p,
		})
	}

	n.Value = nil
	n.Children = f.Object()

	o[k] = n

	return nil
}