"! <p class="shorttext synchronized">Gate for ADT over HTTP (checks Z_ADT_HTTP)</p>
"! First handler in the /sap/bc/adt handler list, ahead of
"! CL_ADT_WB_RES_APP. Lets a request through only when the user holds
"! Z_ADT_HTTP; everyone else gets 403. Eclipse ADT talks RFC
"! (SADT_REST_RFC_ENDPOINT) and never reaches this class, so it only
"! governs HTTP clients - MCP servers and other non-Eclipse tools.
"! The vsp bridge (ZCL_VSP_APC_HANDLER) applies the same check through
"! IS_AUTHORIZED, so both doors stay on one rule.
CLASS zcl_adt_http_gate DEFINITION
  PUBLIC
  FINAL
  CREATE PUBLIC.

  PUBLIC SECTION.
    INTERFACES if_http_extension.

    "! <p class="shorttext synchronized">May the current user use ADT over HTTP / the bridge</p>
    CLASS-METHODS is_authorized
      RETURNING VALUE(rv_authorized) TYPE abap_bool.

  PRIVATE SECTION.
    CONSTANTS:
      c_auth_object   TYPE xuobject VALUE 'Z_ADT_HTTP',
      c_actvt_execute TYPE activ_auth VALUE '16'.

ENDCLASS.



CLASS zcl_adt_http_gate IMPLEMENTATION.

  METHOD is_authorized.
    AUTHORITY-CHECK OBJECT c_auth_object
      ID 'ACTVT' FIELD c_actvt_execute.
    rv_authorized = xsdbool( sy-subrc = 0 ).
  ENDMETHOD.


  METHOD if_http_extension~handle_request.
    IF is_authorized( ) = abap_true.
      " Hand the request on to the ADT handler
      if_http_extension~flow_rc =
        if_http_extension=>co_flow_ok_others_mand.
      RETURN.
    ENDIF.

    " Fail closed: also lands here while the object does not exist yet
    server->response->set_status( code = 403 reason = 'Forbidden' ).
    server->response->set_header_field(
      name  = 'Content-Type'
      value = 'text/plain; charset=utf-8' ).
    server->response->set_cdata(
      |ADT over HTTP is not authorized for user { sy-uname } | &&
      |(authorization object { c_auth_object }).| ).
    if_http_extension~flow_rc = if_http_extension=>co_flow_ok.
  ENDMETHOD.

ENDCLASS.