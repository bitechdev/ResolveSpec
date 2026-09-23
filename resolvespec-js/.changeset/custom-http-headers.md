---
"@warkypublic/resolvespec-js": patch
---

Forward custom ClientConfig headers on every ResolveSpec and HeaderSpec request. Merge headers case-insensitively and isolate cached clients by URL and effective headers, including authentication and tenant headers.
