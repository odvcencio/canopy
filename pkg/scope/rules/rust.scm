;; Function definitions
(function_item
  name: (identifier) @def.function)

;; Function return types
(function_item
  name: (identifier) @def.function
  return_type: (_) @def.function.return)

;; Impl blocks — methods
(function_item
  name: (identifier) @def.method)

;; Struct definitions
(struct_item
  name: (type_identifier) @def.type)

;; Struct fields
(field_declaration
  name: (field_identifier) @def.field
  type: (_) @def.field.type)

;; Enum definitions
(enum_item
  name: (type_identifier) @def.type)

;; Trait definitions
(trait_item
  name: (type_identifier) @def.interface)

;; Type alias
(type_item
  name: (type_identifier) @def.type)

;; Constant
(const_item
  name: (identifier) @def.constant)

;; Static variable
(static_item
  name: (identifier) @def.variable)

;; Let bindings
(let_declaration
  pattern: (identifier) @def.variable)

;; Let bindings with type
(let_declaration
  pattern: (identifier) @def.variable
  type: (_) @def.variable.type)

;; Use declarations (imports)
(use_declaration
  argument: (_) @def.import)

;; Parameters
(parameter
  pattern: (identifier) @def.param)

;; Parameters with type
(parameter
  pattern: (identifier) @def.param
  type: (_) @def.param.type)

;; References — method calls
(call_expression
  function: (identifier) @ref.call)

;; References — field access
(field_expression
  field: (field_identifier) @ref.member)

;; References — plain identifiers
(identifier) @ref
