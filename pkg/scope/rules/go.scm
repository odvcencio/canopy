;; Function definitions
(function_declaration
  (identifier) @def.function) @scope.function

;; Method definitions
(method_declaration
  (field_identifier) @def.method) @scope.function

;; Variable declarations with type annotation
(var_spec
  (identifier) @def.variable
  (type_identifier) @def.variable.type)

;; Variable declarations without type (inferred)
(var_spec
  (identifier) @def.variable.notype)

;; Short variable declarations
(short_var_declaration
  (expression_list
    (identifier) @def.variable))

;; Constant declarations
(const_spec
  (identifier) @def.constant)

;; Type declarations
(type_spec
  (type_identifier) @def.type)

;; Parameters
(parameter_declaration
  (identifier) @def.param
  (type_identifier) @def.param.type)

;; Import declarations — plain
(import_spec
  (interpreted_string_literal) @def.import.path)

;; Import declarations — aliased
(import_spec
  (package_identifier) @def.import.alias
  (interpreted_string_literal) @def.import.aliased.path)

;; Block scopes
(block) @scope.block

;; References — selector expressions (foo.Bar)
(selector_expression
  (identifier) @ref.operand
  (field_identifier) @ref.member)

;; References — plain identifiers in call position
(call_expression
  (identifier) @ref.call)

;; References — plain identifiers
(identifier) @ref

;; Function return types
(function_declaration
  name: (identifier) @def.function
  result: (type_identifier) @def.function.return)

;; Method return types
(method_declaration
  name: (field_identifier) @def.method
  result: (type_identifier) @def.function.return)

;; Method receiver
(method_declaration
  receiver: (parameter_list
    (parameter_declaration
      type: (_) @def.method.receiver))
  name: (field_identifier) @def.method)

;; Struct field declarations
(field_declaration
  name: (field_identifier) @def.field
  type: (_) @def.field.type)
