"! <p class="shorttext synchronized">Gate for ADT over HTTP (checks Z_ADT_HTTP)</p>
"! First handler in the /sap/bc/adt handler list, ahead of
"! CL_ADT_WB_RES_APP. Lets a request through only when the user holds
"! Z_ADT_HTTP; everyone else gets 403. Eclipse ADT talks RFC
"! (SADT_REST_RFC_ENDPOINT) and never reaches this class, so it only
"! governs HTTP clients - MCP servers and other non-Eclipse tools.
"! The vsp bridge (ZCL_VSP_APC_HANDLER) applies the same check through
"! IS_AUTHORIZED, so both doors stay on one rule.
"! The authorization alone is not the approval: SAP_ALL picks up Z_ADT_HTTP
"! whenever it is regenerated (an XPRA in every support package, all
"! clients). Access is approved by assigning profile ZADT_ACC, so the user
"! must hold that assignment as well.
"! Data preview runs the SQL it is sent with the logon client's table
"! authorizations, whatever client the SQL names - SAP checks nothing for
"! CLIENT SPECIFIED. Reading other clients therefore needs its own approval
"! (Z_ADT_XCLI plus profile ZADT_XCLI); everyone else logs on to the client.
CLASS zcl_adt_http_gate DEFINITION
  PUBLIC
  FINAL
  CREATE PUBLIC.

  PUBLIC SECTION.
    INTERFACES if_http_extension.

    "! <p class="shorttext synchronized">May the current user use ADT over HTTP / the bridge</p>
    CLASS-METHODS is_authorized
      RETURNING VALUE(rv_authorized) TYPE abap_bool.

    "! <p class="shorttext synchronized">May the current user read other clients in data preview</p>
    CLASS-METHODS is_cross_client_authorized
      RETURNING VALUE(rv_authorized) TYPE abap_bool.

    "! <p class="shorttext synchronized">Does this Open SQL statement read other clients</p>
    CLASS-METHODS is_cross_client_sql
      IMPORTING iv_sql                 TYPE string
      RETURNING VALUE(rv_cross_client) TYPE abap_bool.

  PRIVATE SECTION.
    CONSTANTS:
      c_auth_object      TYPE xuobject VALUE 'Z_ADT_HTTP',
      c_actvt_execute    TYPE activ_auth VALUE '16',
      c_approval_profile TYPE xuprofile VALUE 'ZADT_ACC',
      c_xcli_object      TYPE xuobject VALUE 'Z_ADT_XCLI',
      c_actvt_display    TYPE activ_auth VALUE '03',
      c_xcli_profile     TYPE xuprofile VALUE 'ZADT_XCLI'.

    CLASS-METHODS has_profile
      IMPORTING iv_profile    TYPE xuprofile
      RETURNING VALUE(rv_has) TYPE abap_bool.

    METHODS refuse
      IMPORTING io_server TYPE REF TO if_http_server
                iv_text   TYPE string.

ENDCLASS.



CLASS zcl_adt_http_gate IMPLEMENTATION.

  METHOD is_authorized.
    AUTHORITY-CHECK OBJECT c_auth_object
      ID 'ACTVT' FIELD c_actvt_execute.
    IF sy-subrc <> 0.
      RETURN.
    ENDIF.

    " SAP_ALL passes the check above once regenerated, without anyone having
    " approved anything. The approval is the profile assignment itself, so a
    " direct assignment of ZADT_ACC in this client is required too.
    rv_authorized = has_profile( c_approval_profile ).
  ENDMETHOD.


  METHOD is_cross_client_authorized.
    AUTHORITY-CHECK OBJECT c_xcli_object
      ID 'ACTVT' FIELD c_actvt_display.
    IF sy-subrc <> 0.
      RETURN.
    ENDIF.

    " Same reasoning as IS_AUTHORIZED: SAP_ALL is not an approval.
    rv_authorized = has_profile( c_xcli_profile ).
  ENDMETHOD.


  METHOD is_cross_client_sql.
    DATA lt_source     TYPE string_table.
    DATA lt_tokens     TYPE stokesx_tab.
    DATA lt_statements TYPE sstmnt_tab.
    DATA lv_previous   TYPE string.

    " Tokens, as the data preview's own authority check reads the statement:
    " comments drop out and a literal stays one token with its quotes, so
    " neither can fake or hide the two words.
    DATA(lv_sql) = replace( val  = iv_sql
                            sub  = cl_abap_char_utilities=>cr_lf
                            with = cl_abap_char_utilities=>newline
                            occ  = 0 ).
    SPLIT lv_sql AT cl_abap_char_utilities=>newline INTO TABLE lt_source.
    SCAN ABAP-SOURCE lt_source TOKENS INTO lt_tokens STATEMENTS INTO lt_statements
      WITH ANALYSIS.

    LOOP AT lt_tokens INTO DATA(ls_token).
      DATA(lv_word) = to_upper( ls_token-str ).
      IF lv_word CP '##*'.
        CONTINUE. " pragma
      ENDIF.
      IF ( lv_previous = 'CLIENT' AND lv_word = 'SPECIFIED' )
         OR ( lv_previous = 'USING' AND ( lv_word = 'CLIENT' OR lv_word = 'CLIENTS' OR lv_word = 'ALL' ) ).
        rv_cross_client = abap_true.
        RETURN.
      ENDIF.
      lv_previous = lv_word.
    ENDLOOP.

    " Plain text as well, literals removed, for bodies the scanner does not
    " split cleanly (SQL wrapped in markup). Errs towards refusing.
    DATA(lv_text) = to_upper( lv_sql ).
    REPLACE ALL OCCURRENCES OF REGEX `'[^']*'|``[^``]*``` IN lv_text WITH ``.
    FIND REGEX `\bCLIENT\s+SPECIFIED\b|\bUSING\s+(ALL\s+)?CLIENTS?\b` IN lv_text.
    rv_cross_client = xsdbool( sy-subrc = 0 ).
  ENDMETHOD.


  METHOD has_profile.
    SELECT SINGLE profile FROM ust04
      WHERE bname   = @sy-uname
        AND profile = @iv_profile
      INTO @DATA(lv_profile).
    rv_has = xsdbool( sy-subrc = 0 ).
  ENDMETHOD.


  METHOD refuse.
    io_server->response->set_status( code = 403 reason = 'Forbidden' ).
    io_server->response->set_header_field(
      name  = 'Content-Type'
      value = 'text/plain; charset=utf-8' ).
    io_server->response->set_cdata( iv_text ).
    if_http_extension~flow_rc = if_http_extension=>co_flow_ok.
  ENDMETHOD.


  METHOD if_http_extension~handle_request.
    IF is_authorized( ) = abap_false.
      " Fail closed: also lands here while the object does not exist yet
      refuse( io_server = server
              iv_text   = |ADT over HTTP is not authorized for user { sy-uname } | &&
                          |(profile { c_approval_profile }, authorization object { c_auth_object }).| ).
      RETURN.
    ENDIF.

    " Data preview (/datapreview/freestyle, /ddic, /cds) runs the SQL in the
    " body with this client's table authorizations, for any client it names.
    " The path is decoded and matched loosely so no spelling of it slips by.
    DATA(lv_path) = cl_http_utility=>unescape_url(
                      escaped = server->request->get_header_field( '~path' ) ).
    IF lv_path CS 'datapreview'
       AND is_cross_client_sql( server->request->get_cdata( ) ) = abap_true
       AND is_cross_client_authorized( ) = abap_false.
      refuse( io_server = server
              iv_text   = |Reading other clients (CLIENT SPECIFIED) is not authorized for user { sy-uname } | &&
                          |(profile { c_xcli_profile }, authorization object { c_xcli_object }). | &&
                          |Log on to that client instead.| ).
      RETURN.
    ENDIF.

    " Hand the request on to the ADT handler
    if_http_extension~flow_rc = if_http_extension=>co_flow_ok_others_mand.
  ENDMETHOD.

ENDCLASS.