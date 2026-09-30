# Clients

| Dir | Language | Specs | Verified |
|---|---|---|---|
| `resolvespec-js` | TypeScript | ResolveSpec, HeaderSpec, FunctionSpec, WebSocketSpec | yes |
| `resolvespec-python` | Python >= 3.11 | ResolveSpec, HeaderSpec, FunctionSpec, WebSocketSpec | yes (61 tests) |
| `resolvespec-go` | Go | ResolveSpec, FunctionSpec | yes (`go test`) |
| `resolvespec-rs` | Rust | ResolveSpec, FunctionSpec | yes (`cargo test`) |
| `resolvespec-cs` | C# (.NET 8) | ResolveSpec, FunctionSpec | **not compiled** |
| `resolvespec-dart` | Dart / Flutter | ResolveSpec, FunctionSpec | yes (`dart test`) |

Wire behaviour is identical across clients; FunctionSpec server quirks are listed in each README.
