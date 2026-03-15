;; Function definitions
(function_definition (identifier) @def.function)

;; Class definitions
(class_definition (identifier) @def.class)

;; Import statement: import os
(import_statement (dotted_name (identifier) @def.import))

;; From-import: from X import Y — skip the module dotted_name, capture imported name
(import_from_statement (dotted_name) (dotted_name (identifier) @def.import))

;; Variable assignments
(assignment (identifier) @def.variable)

;; References
(identifier) @ref

;; Function return type annotations
(function_definition
  name: (identifier) @def.function
  return_type: (type) @def.function.return)

;; Class inheritance
(class_definition
  name: (identifier) @def.class
  superclasses: (argument_list
    (identifier) @def.class.extends))

;; Method definitions (inside class)
(function_definition
  name: (identifier) @def.method)

;; Parameter type annotations
(typed_parameter
  (identifier) @def.param
  type: (type) @def.param.type)

;; Typed default parameters
(typed_default_parameter
  name: (identifier) @def.param
  type: (type) @def.param.type)
