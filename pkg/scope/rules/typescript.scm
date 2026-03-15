;; Function declarations
(function_declaration (identifier) @def.function)

;; Class declarations (uses type_identifier, not identifier)
(class_declaration (type_identifier) @def.class)

;; Interface declarations
(interface_declaration (type_identifier) @def.interface)

;; Type alias declarations
(type_alias_declaration (type_identifier) @def.type)

;; Enum declarations
(enum_declaration (identifier) @def.type)

;; Variable declarations (const/let/var)
(variable_declarator (identifier) @def.variable)

;; Import specifiers: import { useState } from 'react'
(import_specifier (identifier) @def.import)

;; Method definitions
(method_definition (property_identifier) @def.method)

;; References
(identifier) @ref
(type_identifier) @ref

;; Function return type annotations
(function_declaration
  name: (identifier) @def.function
  return_type: (type_annotation
    (_) @def.function.return))

;; Method return types
(method_definition
  name: (property_identifier) @def.method
  return_type: (type_annotation
    (_) @def.function.return))

;; Class heritage (extends)
(class_declaration
  name: (type_identifier) @def.class
  (class_heritage
    (extends_clause
      value: (identifier) @def.class.extends)))

;; Interface method signatures
(method_signature
  name: (property_identifier) @def.method
  return_type: (type_annotation
    (_) @def.function.return))

;; Property signatures in interfaces/classes
(property_signature
  name: (property_identifier) @def.field
  type: (type_annotation
    (_) @def.field.type))

;; Public field declarations in classes
(public_field_definition
  name: (property_identifier) @def.field
  type: (type_annotation
    (_) @def.field.type))

;; Required/optional parameters with types
(required_parameter
  pattern: (identifier) @def.param
  type: (type_annotation
    (_) @def.param.type))

(optional_parameter
  pattern: (identifier) @def.param
  type: (type_annotation
    (_) @def.param.type))
