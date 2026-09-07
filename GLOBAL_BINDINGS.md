# Global binding provenance

LoadGlobal already has BindingKind, Source and Imported fields, but lowering
filled every identifier read with Global and left both strings empty. The fields
were a declared capability rather than an implemented one. A consumer could not
distinguish an imported hook from an unresolved name or a module-local binding.

Lowering now retains declaration provenance supplied by the resident checker:
named imports (including their exported name), default imports, namespace imports
and module-local bindings. An unresolved global stays Global. Local and captured
values still take their earlier LoadLocal/LoadContext paths, so a parameter
shadowing an import does not become a global load.

Seven fixtures distinguish these cases. The five provenance cases fail against
the old lowering and pass with the repair; the genuine-global and shadow controls
pass on both sides. This is a metadata prerequisite, not a custom-hook signature
repair. No hook effects or validator conditions change in this unit.
