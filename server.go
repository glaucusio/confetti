package bigstruct

import (
	"context"
	"errors"
	"fmt"
	"path"

	"github.com/rjeczalik/bigstruct/big"
	"github.com/rjeczalik/bigstruct/big/codec"
	"github.com/rjeczalik/bigstruct/internal/bigstruct"
	"github.com/rjeczalik/bigstruct/storage"
	"github.com/rjeczalik/bigstruct/storage/model"

	"gorm.io/gorm"
)

var (
	structOnly = func(tx storage.Gorm) storage.Gorm { return tx.WithDB(tx.DB.Where("`type` = ?", "struct")) }
)

type Transport interface {
	Do(context.Context, *Op) error
}

type Func func(context.Context, *Op) error

func (fn Func) Do(ctx context.Context, op *Op) error { return fn(ctx, op) }

type Server struct {
	Storage *storage.Gorm
	Codec   big.Codec
}

func (s *Server) Do(ctx context.Context, op *Op) error {
	switch op.Type {
	case "LIST":
		return s.doList(ctx, op)
	case "GET":
		return s.doGet(ctx, op)
	case "SET":
		return s.doSet(ctx, op)
	case "DEBUG":
		return s.doDebug(ctx, op)
	default:
		return fmt.Errorf("unsupported op type: %q", op.Type)
	}
}

func (s *Server) doList(ctx context.Context, op *Op) error {
	return s.Storage.Transaction(s.txDoList(ctx, op))
}

func (s *Server) txDoList(ctx context.Context, op *Op) storage.Func {
	var (
		prefixes = op.Struct.Fields().Keys()
	)

	if len(prefixes) == 0 {
		prefixes = append(prefixes, big.Prefix)
	}

	return func(tx storage.Gorm) error {
		var (
			obj bigstruct.Object
		)

		if err := s.txObject(ctx, op, &obj)(tx); err != nil {
			return fmt.Errorf("error building object: %w", err)
		}

		tx = tx.WithScopes(structOnly)

		for _, prefix := range prefixes {
			if err := obj.LoadSchema(tx, prefix); err != nil {
				return fmt.Errorf("error loading schema for %q prefix: %w", prefix, err)
			}
		}

		op.Struct = obj.Schemas().Fields().Struct()

		return nil
	}
}

func (s *Server) doGet(ctx context.Context, op *Op) error {
	return s.Storage.Transaction(s.txDoGet(ctx, op))
}

func (s *Server) txDoGet(ctx context.Context, op *Op) storage.Func {
	var (
		keys = op.Struct.Fields().Keys()
	)

	if len(keys) == 0 {
		keys = append(keys, big.Prefix)
	}

	return func(tx storage.Gorm) (err error) {
		var obj bigstruct.Object

		if err = s.txObject(ctx, op, &obj)(tx); err != nil {
			return fmt.Errorf("error building object: %w", err)
		}

		for _, key := range keys {
			if err = obj.LoadSchema(tx, key); err != nil {
				return fmt.Errorf("error loading schema for %q key: %w", key, err)
			}

			if err = obj.LoadValue(tx, key); err != nil {
				return fmt.Errorf("error loading values for %q key: %w", key, err)
			}
		}

		op.Struct = obj.Fields().Merge().ShakeTypes().Shake()

		if op.Encode {
			if err = op.Struct.Encode(ctx, s.codec()); err != nil {
				return fmt.Errorf("error encoding struct: %w", err)
			}
		}

		return nil
	}
}

func (s *Server) doSet(ctx context.Context, op *Op) error {
	return s.Storage.Transaction(s.txDoSet(ctx, op))
}

func (s *Server) txDoSet(ctx context.Context, op *Op) storage.Func {
	return func(tx storage.Gorm) (err error) {
		var obj bigstruct.Object

		if err = s.txObject(ctx, op, &obj)(tx); err != nil {
			return err
		}

		if err = obj.LoadSchema(tx, big.Prefix); err != nil {
			return err
		}

		var (
			schema = obj.Schemas().Fields().Struct()
		)

		// validate schema

		err = op.Struct.Walk(func(key string, o big.Struct) error {
			var (
				k = path.Base(key)
				n = o[k]
			)

			if n.Type != "" {
				switch t := schema.TypeAt(key); {
				case t == "":
					// ok
				case t == n.Type:
					n.Type = "" // strip
					o[k] = n
				default:
					return fmt.Errorf("cannot override existing schema %q for %q key with %q type (%#v)", t, key, n.Type, n.Value)
				}
			}

			return nil
		})

		if err != nil {
			return err
		}

		var (
			value = op.Struct.Copy().Raw()
		)

		// validate key

		err = value.ForEach(func(key string, o big.Struct) error {
			var (
				d, k = path.Split(key)
				n    = o[k]
			)

			if n.Value == nil {
				return nil
			}

			if _, ok := schema.At(d)[k]; !ok {
				return fmt.Errorf("the key %q (%T) does not exist in schema", key, n.Value)
			}

			return nil
		})

		if err != nil {
			return err
		}

		// validate value

		if err := schema.Merge(value).Decode(ctx, s.codec()); err != nil {
			return err
		}

		var (
			f = op.Struct.Fields()
			v = model.MakeValues(obj.Namespace, f)
			s = model.MakeSchemas(obj.Namespace, f)
		)

		if err := tx.UpsertSchemas(s); err != nil {
			return err
		}

		if err := tx.UpsertValues(v); err != nil {
			return err
		}

		return nil
	}
}

func (s *Server) doDebug(ctx context.Context, op *Op) error {
	return s.Storage.Transaction(s.txDoDebug(ctx, op))
}

func (s *Server) txDoDebug(ctx context.Context, op *Op) storage.Func {
	var (
		keys = op.Struct.Fields().Keys()
	)

	if len(keys) == 0 {
		keys = append(keys, big.Prefix)
	}

	return func(tx storage.Gorm) (err error) {
		var obj bigstruct.Object

		if err = s.txObject(ctx, op, &obj)(tx); err != nil {
			return fmt.Errorf("error building object: %w", err)
		}

		for _, key := range keys {
			if err = obj.LoadSchema(tx, key); err != nil {
				return fmt.Errorf("error loading schema for %q key: %w", key, err)
			}

			if err = obj.LoadValue(tx, key); err != nil {
				return fmt.Errorf("error loading values for %q key: %w", key, err)
			}
		}

		op.Struct = nil
		op.Debug.Schemas = obj.Schemas()
		op.Debug.Values = obj.Values()

		return nil
	}
}

func (s *Server) object(ctx context.Context, op *Op) (*bigstruct.Object, error) {
	var obj bigstruct.Object

	return &obj, s.Storage.Transaction(s.txObject(ctx, op, &obj))
}

func (s *Server) txObject(ctx context.Context, op *Op, out *bigstruct.Object) storage.Func {
	return func(tx storage.Gorm) (err error) {
		obj := bigstruct.Object{
			Index:     op.Index,
			Namespace: op.Namespace,
		}

		if op.Namespace != nil {
			obj.Namespace, err = tx.Namespace(op.Namespace.Ref())
			if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
				return fmt.Errorf("error loading %q namespace: %w", op.Namespace.Ref(), err)
			}
		}

		ns, err := s.buildNamespaces(ctx, tx, &obj)
		if err != nil {
			return fmt.Errorf("error building namespaces: %w", err)
		}

		for _, n := range ns {
			obj.Scopes = append(obj.Scopes, bigstruct.Scope{
				Namespace: n,
			})
		}

		*out = obj

		return nil
	}
}

func (s *Server) buildNamespaces(ctx context.Context, tx storage.Gorm, obj *bigstruct.Object) (model.Namespaces, error) {
	ns, err := tx.ListNamespaces()
	if err != nil {
		return nil, err
	}

	n, err := s.indexNamespaces(ctx, tx, ns, obj)
	if err != nil {
		return nil, err
	}

	return ns[:n], nil
}

func (s *Server) indexNamespaces(ctx context.Context, tx storage.Gorm, ns model.Namespaces, obj *bigstruct.Object) (int, error) {
	var (
		m = obj.Index.Index.Map()
		n = len(ns)
	)

	if len(m) == 0 {
		err := tx.DB.Where("`name` = ? AND `property` = ?", obj.Index.Name, obj.Index.Property).First(obj.Index).Error
		if err != nil {
			return 0, fmt.Errorf("error looking up static index for %q: %w", obj.Index.Ref(), err)
		}

		m = obj.Index.Index.Map()
	}

	for i, ns := range ns {
		var prop string

		if v, ok := m[ns.Name]; ok && v != "" {
			prop = fmt.Sprint(v)
		}

		if err := ns.SetProperty(prop); err != nil {
			return 0, fmt.Errorf(
				"unable to set property %v for namespace %q indexed via %q: %w",
				prop, ns.Name, obj.Index.Ref(), err,
			)
		}

		if obj.Namespace != nil && obj.Namespace.Name == ns.Name {
			n = i + 1
			break
		}
	}

	return n, nil
}

func (s *Server) codec() big.Codec {
	if s.Codec != nil {
		return s.Codec
	}
	return codec.Default
}
