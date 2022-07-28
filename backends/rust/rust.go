package rust

import (
	"fmt"
	"io/fs"
	"io/ioutil"
	"lugmac/backends"
	"lugmac/modules"
	"lugmac/typechecking"
	"os"
	"path"

	"github.com/iancoleman/strcase"
	"github.com/urfave/cli/v2"
)

type RustBackend struct {
}

func contains[T comparable](a []T, b T) bool {
	for _, item := range a {
		if item == b {
			return true
		}
	}
	return false
}

func (r *RustBackend) RustTypeOf(lugma typechecking.Type, module typechecking.Path, in *typechecking.Context) string {
	switch k := lugma.(type) {
	case typechecking.PrimitiveType:
		switch k {
		case typechecking.UInt8:
			return "u8"
		case typechecking.UInt16:
			return "u16"
		case typechecking.UInt32:
			return "u32"
		case typechecking.Int8:
			return "i8"
		case typechecking.Int16:
			return "i16"
		case typechecking.Int32:
			return "i32"
		case typechecking.Int64:
			return "i64"
		case typechecking.UInt64:
			return "u64"
		case typechecking.String:
			return "String"
		case typechecking.Bytes:
			return "Vec<u8>"
		case typechecking.Bool:
			return "bool"
		default:
			panic("unhandled primitive " + k.String())
		}
	case typechecking.ArrayType:
		return fmt.Sprintf("Vec<%s>", r.RustTypeOf(k.Element, module, in))
	case typechecking.DictionaryType:
		return fmt.Sprintf("HashMap<%s, %s>", r.RustTypeOf(k.Key, module, in), r.RustTypeOf(k.Element, module, in))
	case typechecking.OptionalType:
		return fmt.Sprintf("Optional<%s>", r.RustTypeOf(k.Element, module, in))
	case *typechecking.Struct, *typechecking.Enum:
		if k.Path().ModulePath == module.ModulePath {
			return k.String()
		}
		return fmt.Sprintf("TODO")
	default:
		panic("unhandled " + k.String())
	}
}

func (r *RustBackend) GenerateTypes(mod *typechecking.Module, in *typechecking.Context) (string, error) {
	build := backends.Filebuilder{}

	build.Add(`use serde::{Serialize, Deserialize};`)

	for _, item := range mod.Structs {
		build.Add(`#[derive(Serialize, Deserialize, Debug)]`)
		build.AddI("pub struct %s {", item.ObjectName())
		for _, field := range item.Fields {
			if field.ObjectName() != strcase.ToSnake(field.ObjectName()) {
				build.Add(`#[serde(rename = "%s")] `, field.ObjectName())
			}
			build.Add(`pub %s: %s,`, strcase.ToSnake(field.ObjectName()), r.RustTypeOf(field.Type, mod.Path(), in))
		}
		build.AddD("}")
	}
	for _, item := range mod.Enums {
		build.Add(`#[derive(Serialize, Deserialize, Debug)]`)
		build.AddI("pub enum %s {", item.ObjectName())
		simple := item.Simple()
		for _, esac := range item.Cases {
			if esac.ObjectName() != strcase.ToCamel(esac.ObjectName()) {
				build.Add(`#[serde(rename = "%s")] `, esac.ObjectName())
			}
			if simple {
				build.Add(`%s,`, strcase.ToCamel(esac.ObjectName()))
			} else {
				build.AddE(`%s { `, strcase.ToCamel(esac.ObjectName()))
				for _, field := range esac.Fields {
					if field.ObjectName() != strcase.ToSnake(field.ObjectName()) {
						build.AddK(`#[serde(rename = "%s")] `, field.ObjectName())
					}
					build.AddK(`%s: %s,`, strcase.ToSnake(field.ObjectName()), r.RustTypeOf(field.Type, mod.Path(), in))
				}
				build.AddK(` }`)
				build.AddK(`, `)
				build.AddNL()
				// build.Add(`"%s" |`, esac.ObjectName())
			}
		}
		build.AddD(`}`)
	}
	for _, item := range mod.Flagsets {
		build.Add(`export type %s = string`, item.ObjectName())
	}

	return build.String(), nil
}

func (r *RustBackend) GenerateCommand() *cli.Command {
	possible := []string{"server", "client"}

	return &cli.Command{
		Name:    "rust",
		Aliases: []string{"rs"},
		Usage:   "Generate TypeScript modules for Lugma",
		Flags: append(backends.StandardFlags, []cli.Flag{
			&cli.StringSliceFlag{
				Name:  "types",
				Usage: "The types of code to generate",
				Value: cli.NewStringSlice(possible...),
			},
		}...),
		Action: func(cCtx *cli.Context) error {
			w, err := modules.LoadWorkspaceFrom(cCtx.String("workspace"))
			if err != nil {
				return err
			}
			err = w.GenerateModules()
			if err != nil {
				return err
			}

			outdir := cCtx.String("outdir")
			err = os.MkdirAll(path.Join(outdir), 0750)
			if err != nil {
				return err
			}

			for _, prod := range w.Module.Products {
				mod := w.KnownModules[prod.Name]

				result, err := r.GenerateTypes(mod, w.Context)
				if err != nil {
					return err
				}

				err = ioutil.WriteFile(path.Join(outdir, mod.Name+"_types.rs"), []byte(result), fs.ModePerm)
				if err != nil {
					return err
				}
			}

			types := cCtx.StringSlice("types")
			if contains(types, "client") {
				// for _, prod := range w.Module.Products {
				// 	mod := w.KnownModules[prod.Name]

				// 	result, err := r.GenerateClient(mod, w.Context)
				// 	if err != nil {
				// 		return err
				// 	}

				// 	err = ioutil.WriteFile(path.Join(outdir, mod.Name+"_client.rs"), []byte(result), fs.ModePerm)
				// 	if err != nil {
				// 		return err
				// 	}
				// }
			}
			if contains(types, "server") {
				// for _, prod := range w.Module.Products {
				// 	mod := w.KnownModules[prod.Name]

				// 	result, err := r.GenerateServer(mod, w.Context)
				// 	if err != nil {
				// 		return err
				// 	}

				// 	err = ioutil.WriteFile(path.Join(outdir, mod.Name+"_server.rs"), []byte(result), fs.ModePerm)
				// 	if err != nil {
				// 		return err
				// 	}
				// }
			}

			return nil
		},
	}
}

func init() {
	backends.RegisterBackend(&RustBackend{})
}
