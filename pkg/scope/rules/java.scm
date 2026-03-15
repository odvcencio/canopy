;; Class declarations
(class_declaration
  name: (identifier) @def.class)

;; Class with superclass
(class_declaration
  name: (identifier) @def.class
  (superclass
    (type_identifier) @def.class.extends))

;; Interface declarations
(interface_declaration
  name: (identifier) @def.interface)

;; Method declarations
(method_declaration
  name: (identifier) @def.method)

;; Method return types
(method_declaration
  type: (_) @def.function.return
  name: (identifier) @def.method)

;; Constructor declarations
(constructor_declaration
  name: (identifier) @def.method)

;; Field declarations
(field_declaration
  declarator: (variable_declarator
    name: (identifier) @def.field))

;; Local variable declarations
(local_variable_declaration
  declarator: (variable_declarator
    name: (identifier) @def.variable))

;; Import declarations
(import_declaration
  (_) @def.import)

;; Parameters
(formal_parameter
  name: (identifier) @def.param)

;; Parameter types
(formal_parameter
  type: (_) @def.param.type
  name: (identifier) @def.param)

;; Enum declarations
(enum_declaration
  name: (identifier) @def.type)

;; References — method calls
(method_invocation
  name: (identifier) @ref.call)

;; References — field access
(field_access
  field: (identifier) @ref.member)

;; References — identifiers
(identifier) @ref
