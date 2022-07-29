package swift

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

type SwiftBackend struct {
}

func contains[T comparable](a []T, b T) bool {
	for _, item := range a {
		if item == b {
			return true
		}
	}
	return false
}

// GenerateCommand implements backends.Backend
func (swift *SwiftBackend) GenerateCommand() *cli.Command {
	possible := []string{"server", "client"}
	return &cli.Command{
		Name:  "swift",
		Usage: "Generate Swift modules for Lugma",
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

				result, err := swift.GenerateTypes(mod, w.Context)
				if err != nil {
					return err
				}

				err = ioutil.WriteFile(path.Join(outdir, mod.Name+".types.swift"), []byte(result), fs.ModePerm)
				if err != nil {
					return err
				}
			}

			types := cCtx.StringSlice("types")
			if contains(types, "client") {
				for _, prod := range w.Module.Products {
					mod := w.KnownModules[prod.Name]

					result, err := swift.GenerateClient(mod, w.Context)
					if err != nil {
						return err
					}

					err = ioutil.WriteFile(path.Join(outdir, mod.Name+".client.swift"), []byte(result), fs.ModePerm)
					if err != nil {
						return err
					}
				}
			}
			if contains(types, "server") {
				for _, prod := range w.Module.Products {
					mod := w.KnownModules[prod.Name]

					result, err := swift.GenerateServer(mod, w.Context)
					if err != nil {
						return err
					}

					err = ioutil.WriteFile(path.Join(outdir, mod.Name+".server.swift"), []byte(result), fs.ModePerm)
					if err != nil {
						return err
					}
				}
			}

			return nil
		},
	}
}

func init() {
	backends.RegisterBackend(&SwiftBackend{})
}

func (swift SwiftBackend) GenerateTypes(mod *typechecking.Module, in *typechecking.Context) (string, error) {
	build := backends.Filebuilder{}

	for _, item := range mod.Structs {
		build.AddI("public struct %s%s: Codable {", mod.ObjectName(), item.ObjectName())
		for _, field := range item.Fields {
			build.Add(`var %s: %s`, field.ObjectName(), swift.TypeOf(field.Type, mod.Path(), in))
		}
		build.AddD("}")
	}
	for _, item := range mod.Enums {
		build.AddI("public enum %s%s: Codable {", mod.ObjectName(), item.ObjectName())
		for _, esac := range item.Cases {
			build.AddE(`case %s`, esac.ObjectName())
			if len(esac.Fields) > 0 {
				build.AddK(`(`)
			}
			for idx, field := range esac.Fields {
				build.AddK(`%s: %s`, field.ObjectName(), swift.TypeOf(field.Type, mod.Path(), in))
				if idx < len(esac.Fields)-1 {
					build.AddK(`, `)
				}
			}
			if len(esac.Fields) > 0 {
				build.AddK(`)`)
			}
			build.AddNL()
		}
		build.AddD(`}`)
	}
	for _, item := range mod.Flagsets {
		build.Add(`export type %s = string`, item.ObjectName())
	}

	return build.String(), nil
}

func (swift SwiftBackend) GenerateServer(mod *typechecking.Module, in *typechecking.Context) (string, error) {
	build := backends.Filebuilder{}

	build.Add(`import LugmaVapor`)
	build.Add(`import Vapor`)

	generateArguments := func(args []*typechecking.Field, named bool, forceTrailing bool) {
		for idx, arg := range args {
			if named {
				build.AddK(`%s: %s`, arg.Name, swift.TypeOf(arg.Type, mod.Path(), in))
			} else {
				build.AddK(`%s`, swift.TypeOf(arg.Type, mod.Path(), in))
			}
			if forceTrailing || idx != len(args)-1 {
				build.AddK(`, `)
			}
		}
	}
	generateForwardingArguments := func(args []*typechecking.Field, fieldName bool, prefix string) {
		for idx, arg := range args {
			if fieldName {
				build.AddK(`%s: `, arg.Name)
			}
			build.AddK(`%s%s`, prefix, arg.Name)
			if idx != len(args)-1 {
				build.AddK(`, `)
			}
		}
	}
	generateArgsStruct := func(obj typechecking.Object, args []*typechecking.Field) {
		build.AddI(`fileprivate struct %sArguments: Codable {`, strcase.ToCamel(obj.ObjectName()))
		for _, arg := range args {
			build.Add(`let %s: %s`, arg.ObjectName(), swift.TypeOf(arg.Type, mod.Path(), in))
		}
		build.AddD(`}`)
	}

	for _, stream := range mod.Streams {
		build.AddI(`public protocol %s%s {`, mod.ObjectName(), stream.ObjectName())
		for _, ev := range stream.Events {
			build.AddE(`func send%s(`, strcase.ToCamel(ev.ObjectName()))
			generateArguments(ev.Arguments, true, false)
			build.AddK(`) async throws`)
			build.AddNL()
		}
		for _, sig := range stream.Signals {
			build.AddE(`func on%s(callback: @escaping (`, strcase.ToCamel(sig.ObjectName()))
			generateArguments(sig.Arguments, false, false)
			build.AddK(`) async -> ())`)
			build.AddNL()
		}
		build.AddD(`}`)

		build.AddI(`public class %s%sStream<S: Stream>: %s%s {`, mod.ObjectName(), stream.ObjectName(), mod.ObjectName(), stream.ObjectName())
		build.Add(`let stream: S`)
		build.Add(`public let request: S.Request`)
		build.AddI(`public init(from stream: S, request: S.Request) {`)
		build.Add(`self.stream = stream`)
		build.Add(`self.request = request`)
		build.AddD(`}`)
		for _, ev := range stream.Events {
			generateArgsStruct(ev, ev.Arguments)

			build.AddE(`public func send%s(`, strcase.ToCamel(ev.ObjectName()))
			generateArguments(ev.Arguments, true, false)
			build.AddK(`) async throws {`)
			build.AddNL()
			build.Einzug++

			build.AddE(`try await self.stream.send(event: "%s", item: %sArguments(`, ev.ObjectName(), strcase.ToCamel(ev.ObjectName()))
			generateForwardingArguments(ev.Arguments, true, "")
			build.AddK(`)`)
			build.AddK(`)`)
			build.AddNL()

			build.AddD(`}`)
		}
		for _, sig := range stream.Signals {
			generateArgsStruct(sig, sig.Arguments)

			build.AddE(`public func on%s(callback: @escaping (`, strcase.ToCamel(sig.ObjectName()))
			generateArguments(sig.Arguments, false, false)
			build.AddK(`) async -> ()) {`)
			build.AddNL()
			build.Einzug++

			build.AddI(`self.stream.on(signal: "%s") { (req: %sArguments) in`, sig.ObjectName(), strcase.ToCamel(sig.ObjectName()))

			build.AddE(`await callback(`)
			generateForwardingArguments(sig.Arguments, false, "req.")
			build.AddK(`)`)
			build.AddNL()

			build.AddD(`}`)

			build.AddD(`}`)
		}
		build.AddD(`}`)
	}

	build.AddI(`public protocol %sHandler {`, mod.ObjectName())
	build.Add(`associatedtype T: Transport`)
	for _, fn := range mod.Funcs {
		build.AddE(`func %s(`, fn.ObjectName())
		generateArguments(fn.Arguments, true, true)
		build.AddK(`request: T.Request`)
		build.AddK(`) async throws -> Result<%s, %s>`, swift.TypeOf(fn.Returns, mod.Path(), in), swift.TypeOf(fn.Throws, mod.Path(), in))
		build.AddNL()
	}
	for _, stream := range mod.Streams {
		build.Add(`func on%sOpened(stream: %s%s)`, stream.ObjectName(), mod.ObjectName(), stream.ObjectName())
	}
	build.AddD(`}`)

	for _, fn := range mod.Funcs {
		generateArgsStruct(fn, fn.Arguments)
	}

	build.AddI(`extension %sHandler {`, mod.ObjectName())
	build.AddI(`public func bind(to transport: T) {`)
	for _, fn := range mod.Funcs {
		build.AddI(`transport.bind(method: "%s") { (req: T.Request, args: %sArguments) in`, fn.Path(), strcase.ToCamel(fn.ObjectName()))
		build.AddE(`return try await self.%s(`, fn.ObjectName())
		generateForwardingArguments(fn.Arguments, true, "args.")
		if len(fn.Arguments) > 0 {
			build.AddK(`, `)
		}
		build.AddK(`request: req`)
		build.AddK(`)`)
		build.AddNL()
		build.AddD(`}`)
	}
	for _, stream := range mod.Streams {
		build.AddI(`transport.bind(stream: "%s") { req, stream in`, stream.ObjectName())
		build.Add(`self.on%sOpened(stream: %s%sStream(from: stream, request: req))`, strcase.ToCamel(stream.ObjectName()), mod.ObjectName(), strcase.ToCamel(stream.ObjectName()))
		build.AddD(`}`)
	}
	build.AddD(`}`)
	build.AddD(`}`)

	return build.String(), nil
}

func allNames(mod *typechecking.Module) []string {
	var names []string
	for _, item := range mod.Structs {
		names = append(names, item.ObjectName())
	}
	for _, item := range mod.Enums {
		names = append(names, item.ObjectName())
	}
	for _, item := range mod.Flagsets {
		names = append(names, item.ObjectName())
	}
	return names
}

func (swift SwiftBackend) GenerateClient(mod *typechecking.Module, in *typechecking.Context) (string, error) {
	build := backends.Filebuilder{}

	for _, stream := range mod.Streams {
		build.AddI(`public protocol %s%s {`, mod.ObjectName(), stream.ObjectName())

		for _, ev := range stream.Events {
			build.AddE(`func on%s(callback: @escaping (`, strcase.ToCamel(ev.ObjectName()))
			for idx, arg := range ev.Arguments {
				build.AddK(`_ %s: %s`, arg.ObjectName(), swift.TypeOf(arg.Type, mod.Path(), in))
				if idx != len(ev.Arguments)-1 {
					build.AddK(`, `)
				}
			}
			build.AddK(`) async -> Void)`)
			build.AddNL()
		}

		build.AddD(`}`)

		build.AddI(`public class %sStream%s<T: Transport>: %s%s {`, mod.ObjectName(), stream.ObjectName(), mod.ObjectName(), stream.ObjectName())
		{
			build.Add(`let stream: Stream`)

			build.AddI(`public init(from transport: T, extra: T.Extra? = nil) async throws {`)
			build.Add(`self.stream = try await transport.openStream(endpoint: "%s", extra: extra)`, stream.Path())
			build.AddD(`}`)

			for _, ev := range stream.Events {
				build.AddI(`private class %sArguments: Codable {`, strcase.ToCamel(ev.ObjectName()))
				for _, arg := range ev.Arguments {
					build.Add(`let %s: %s`, arg.ObjectName(), swift.TypeOf(arg.Type, mod.Path(), in))
				}
				build.AddD(`}`)

				build.AddE(`public func on%s(callback: @escaping (`, strcase.ToCamel(ev.ObjectName()))
				for idx, arg := range ev.Arguments {
					build.AddK(`_ %s: %s`, arg.ObjectName(), swift.TypeOf(arg.Type, mod.Path(), in))
					if idx != len(ev.Arguments)-1 {
						build.AddK(`, `)
					}
				}
				build.AddK(`) async -> Void) {`)
				build.AddNL()
				build.Einzug++

				build.AddI(`self.stream.on(event: "%s") { (item: %sArguments) in`, ev.ObjectName(), strcase.ToCamel(ev.ObjectName()))

				build.AddE(`await callback(`)
				for idx, arg := range ev.Arguments {
					build.AddK(`item.%s`, arg.ObjectName())
					if idx != len(ev.Arguments)-1 {
						build.AddK(`, `)
					}
				}
				build.AddK(`)`)
				build.AddNL()

				build.AddD(`}`)

				build.AddD(`}`)
			}
		}
		build.AddD(`}`)
	}

	build.AddI(`public protocol %sClient {`, mod.ObjectName())
	build.Add(`associatedtype T: Transport`)

	for _, fn := range mod.Funcs {
		build.AddE(`func %s(`, fn.ObjectName())
		for _, arg := range fn.Arguments {
			build.AddK(`%s: %s`, arg.ObjectName(), swift.TypeOf(arg.Type, mod.Path(), in))
			build.AddK(`, `)
		}
		build.AddK(`extra: T.Extra?`)
		build.AddK(`)`)
		build.AddK(` async throws -> Result<%s, %s>`, swift.TypeOf(fn.Returns, mod.Path(), in), swift.TypeOf(fn.Throws, mod.Path(), in))
		build.AddNL()
	}
	build.AddD(`}`)

	build.AddI(`public class %sTransportClient<T: Transport>: %sClient {`, mod.ObjectName(), mod.ObjectName())

	build.Add(`let transport: T`)
	build.AddI(`public init(with transport: T) {`)
	build.Add(`self.transport = transport`)
	build.AddD(`}`)

	for _, fn := range mod.Funcs {
		build.AddI(`private struct %sRequest: Codable {`, strcase.ToCamel(fn.ObjectName()))
		for _, arg := range fn.Arguments {
			build.Add(`let %s: %s`, arg.ObjectName(), swift.TypeOf(arg.Type, mod.Path(), in))
		}
		build.AddD(`}`)

		build.AddE(`public func %s(`, fn.ObjectName())
		for _, arg := range fn.Arguments {
			build.AddK(`%s: %s`, arg.ObjectName(), swift.TypeOf(arg.Type, mod.Path(), in))
			build.AddK(`, `)
		}
		build.AddK(`extra: T.Extra? = nil`)
		build.AddK(`)`)
		build.AddK(` async throws -> Result<%s, %s> {`, swift.TypeOf(fn.Returns, mod.Path(), in), swift.TypeOf(fn.Throws, mod.Path(), in))
		build.AddNL()
		build.Einzug++

		build.AddE(`return try await transport.makeRequest(endpoint: "%s", body: %sRequest(`,
			fn.Path(), strcase.ToCamel(fn.ObjectName()),
		)
		for idx, arg := range fn.Arguments {
			build.AddK(`%s: %s`, arg.ObjectName(), arg.ObjectName())
			if idx != len(fn.Arguments)-1 {
				build.AddK(`, `)
			}
		}
		build.AddK(`)`)
		build.AddK(`, extra: extra)`)
		build.AddNL()

		build.AddD(`}`)
	}
	build.AddD(`}`)

	return build.String(), nil
}

func (swift SwiftBackend) TypeOf(lugma typechecking.Type, module typechecking.Path, in *typechecking.Context) string {
	switch k := lugma.(type) {
	case typechecking.PrimitiveType:
		return k.String()
	case typechecking.ArrayType:
		return fmt.Sprintf("[%s]", swift.TypeOf(k.Element, module, in))
	case typechecking.DictionaryType:
		return fmt.Sprintf("[%s: %s]", swift.TypeOf(k.Key, module, in), swift.TypeOf(k.Element, module, in))
	case typechecking.OptionalType:
		return fmt.Sprintf("%s?", swift.TypeOf(k.Element, module, in))
	case *typechecking.Struct, *typechecking.Enum:
		if k.Path().ModulePath == module.ModulePath {
			return k.String()
		}
		return fmt.Sprintf("TODO")
	case nil:
		return "Nothing"
	default:
		panic("unhandled " + k.String())
	}
}
