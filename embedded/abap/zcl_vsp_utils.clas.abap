"! <p class="shorttext synchronized">VSP Utility Class</p>
"! Centralized utilities for JSON handling, parameter extraction, and response building.
"! Eliminates code duplication across VSP service classes.
CLASS zcl_vsp_utils DEFINITION
  PUBLIC
  FINAL
  CREATE PUBLIC.

  PUBLIC SECTION.
    "! Escape a string for safe JSON embedding
    CLASS-METHODS escape_json
      IMPORTING iv_string         TYPE string
      RETURNING VALUE(rv_escaped) TYPE string.

    "! Extract a string parameter from JSON params
    CLASS-METHODS extract_param
      IMPORTING iv_params       TYPE string
                iv_name         TYPE string
      RETURNING VALUE(rv_value) TYPE string.

    "! Extract an integer parameter from JSON params
    CLASS-METHODS extract_param_int
      IMPORTING iv_params       TYPE string
                iv_name         TYPE string
      RETURNING VALUE(rv_value) TYPE i.

    "! Extract a nested JSON object from params
    CLASS-METHODS extract_param_object
      IMPORTING iv_params      TYPE string
                iv_name        TYPE string
      RETURNING VALUE(rv_json) TYPE string.

    "! Build an error response
    CLASS-METHODS build_error
      IMPORTING iv_id              TYPE string
                iv_code            TYPE string
                iv_message         TYPE string
      RETURNING VALUE(rs_response) TYPE zif_vsp_service=>ty_response.

    "! Build a success response
    CLASS-METHODS build_success
      IMPORTING iv_id              TYPE string
                iv_data            TYPE string
      RETURNING VALUE(rs_response) TYPE zif_vsp_service=>ty_response.

    "! Wrap content in JSON object braces
    CLASS-METHODS json_obj
      IMPORTING iv_content      TYPE string
      RETURNING VALUE(rv_json) TYPE string.

    "! Wrap content in JSON array brackets
    CLASS-METHODS json_arr
      IMPORTING iv_content      TYPE string
      RETURNING VALUE(rv_json) TYPE string.

    "! Build a JSON key-value pair with string value
    CLASS-METHODS json_str
      IMPORTING iv_key         TYPE string
                iv_value       TYPE string
      RETURNING VALUE(rv_json) TYPE string.

    "! Build a JSON key-value pair with integer value
    CLASS-METHODS json_int
      IMPORTING iv_key         TYPE string
                iv_value       TYPE i
      RETURNING VALUE(rv_json) TYPE string.

    "! Build a JSON key-value pair with boolean value
    CLASS-METHODS json_bool
      IMPORTING iv_key         TYPE string
                iv_value       TYPE abap_bool
      RETURNING VALUE(rv_json) TYPE string.

    "! Join multiple JSON fragments with commas
    CLASS-METHODS json_join
      IMPORTING it_parts       TYPE string_table
      RETURNING VALUE(rv_json) TYPE string.

    " Authorization checks for bridge actions that execute code. The
    " bridge runs function modules locally and reports via a plain
    " SUBMIT, and neither performs a check of its own. Each method
    " returns an error text, or an initial string when authorized.
    "! S_RFC check for a function module called through the bridge
    CLASS-METHODS check_rfc_authority
      IMPORTING iv_function     TYPE rs38l_fnam
      RETURNING VALUE(rv_error) TYPE string.

    "! S_PROGRAM / S_PROGNAM check for a report run through the bridge
    CLASS-METHODS check_submit_authority
      IMPORTING iv_report       TYPE progname
      RETURNING VALUE(rv_error) TYPE string.

    "! S_DEVELOP check for a repository change made through the bridge
    CLASS-METHODS check_develop_authority
      IMPORTING iv_object       TYPE trobjtype
                iv_obj_name     TYPE sobj_name
                iv_devclass     TYPE devclass OPTIONAL
                iv_actvt        TYPE activ_auth DEFAULT '02'
      RETURNING VALUE(rv_error) TYPE string.

ENDCLASS.


CLASS zcl_vsp_utils IMPLEMENTATION.

  METHOD escape_json.
    rv_escaped = iv_string.
    REPLACE ALL OCCURRENCES OF '\' IN rv_escaped WITH '\\'.
    REPLACE ALL OCCURRENCES OF '"' IN rv_escaped WITH '\"'.
    REPLACE ALL OCCURRENCES OF cl_abap_char_utilities=>cr_lf IN rv_escaped WITH '\n'.
    REPLACE ALL OCCURRENCES OF cl_abap_char_utilities=>newline IN rv_escaped WITH '\n'.
    REPLACE ALL OCCURRENCES OF cl_abap_char_utilities=>horizontal_tab IN rv_escaped WITH '\t'.
  ENDMETHOD.


  METHOD extract_param.
    DATA lv_name TYPE string.
    lv_name = iv_name.
    CONDENSE lv_name.

    DATA(lv_search) = |"{ lv_name }":|.
    DATA lv_pos TYPE i.
    FIND lv_search IN iv_params MATCH OFFSET lv_pos.
    IF sy-subrc = 0.
      DATA(lv_rest) = iv_params+lv_pos.
      FIND REGEX ':\s*"([^"]*)"' IN lv_rest SUBMATCHES rv_value.
    ENDIF.
  ENDMETHOD.


  METHOD extract_param_int.
    DATA lv_str TYPE string.
    DATA(lv_pattern) = |"{ iv_name }"\\s*:\\s*(\\d+)|.
    FIND REGEX lv_pattern IN iv_params SUBMATCHES lv_str.
    IF sy-subrc = 0.
      rv_value = lv_str.
    ENDIF.
  ENDMETHOD.


  METHOD extract_param_object.
    DATA lv_name TYPE string.
    lv_name = iv_name.
    CONDENSE lv_name.

    DATA(lv_search) = |"{ lv_name }":|.
    DATA lv_pos TYPE i.
    FIND lv_search IN iv_params MATCH OFFSET lv_pos.
    IF sy-subrc <> 0.
      RETURN.
    ENDIF.

    DATA(lv_rest) = iv_params+lv_pos.
    DATA(lv_brace) = find( val = lv_rest sub = '{' ).
    IF lv_brace < 0.
      RETURN.
    ENDIF.

    DATA lv_depth TYPE i.
    DATA lv_i TYPE i.
    lv_i = lv_brace.
    DATA(lv_len) = strlen( lv_rest ).

    WHILE lv_i < lv_len.
      DATA(lv_char) = lv_rest+lv_i(1).
      IF lv_char = '{'.
        lv_depth = lv_depth + 1.
      ELSEIF lv_char = '}'.
        lv_depth = lv_depth - 1.
        IF lv_depth = 0.
          DATA(lv_obj_len) = lv_i - lv_brace + 1.
          rv_json = lv_rest+lv_brace(lv_obj_len).
          RETURN.
        ENDIF.
      ENDIF.
      lv_i = lv_i + 1.
    ENDWHILE.
  ENDMETHOD.


  METHOD build_error.
    rs_response = VALUE #(
      id      = iv_id
      success = abap_false
      error   = json_obj( |"code":"{ iv_code }","message":"{ escape_json( iv_message ) }"| )
    ).
  ENDMETHOD.


  METHOD build_success.
    rs_response = VALUE #(
      id      = iv_id
      success = abap_true
      data    = iv_data
    ).
  ENDMETHOD.


  METHOD json_obj.
    DATA(lv_open) = '{'.
    DATA(lv_close) = '}'.
    rv_json = |{ lv_open }{ iv_content }{ lv_close }|.
  ENDMETHOD.


  METHOD json_arr.
    rv_json = |[{ iv_content }]|.
  ENDMETHOD.


  METHOD json_str.
    rv_json = |"{ iv_key }":"{ escape_json( iv_value ) }"|.
  ENDMETHOD.


  METHOD json_int.
    rv_json = |"{ iv_key }":{ iv_value }|.
  ENDMETHOD.


  METHOD json_bool.
    DATA(lv_val) = COND string( WHEN iv_value = abap_true THEN 'true' ELSE 'false' ).
    rv_json = |"{ iv_key }":{ lv_val }|.
  ENDMETHOD.


  METHOD json_join.
    DATA lv_first TYPE abap_bool VALUE abap_true.
    LOOP AT it_parts INTO DATA(lv_part).
      IF lv_part IS INITIAL.
        CONTINUE.
      ENDIF.
      IF lv_first = abap_false.
        rv_json = |{ rv_json },|.
      ENDIF.
      rv_json = |{ rv_json }{ lv_part }|.
      lv_first = abap_false.
    ENDLOOP.
  ENDMETHOD.


  METHOD check_rfc_authority.
    " The same S_RFC check the RFC runtime performs for a remote call,
    " including the auth/rfc_authority_check setting and SRFC exemptions.
    CALL FUNCTION 'AUTHORITY_CHECK_RFC'
      EXPORTING
        functionname       = iv_function
      EXCEPTIONS
        function_not_exist = 1
        user_dont_exist    = 2
        rfc_no_authority   = 3
        OTHERS             = 4.
    CASE sy-subrc.
      WHEN 0.
        RETURN.
      WHEN 1.
        rv_error = |Function { iv_function } not found|.
      WHEN OTHERS.
        rv_error = |No RFC authorization for function { iv_function }| &&
                   | (S_RFC ACTVT 16, function or its function group)|.
    ENDCASE.
  ENDMETHOD.


  METHOD check_submit_authority.
    " What SA38 checks before starting a report (SUBMIT_REPORT), plus
    " background scheduling, because RunReport always runs a job.
    SELECT SINGLE secu FROM trdir
      WHERE name = @iv_report
      INTO @DATA(lv_secu).
    IF sy-subrc <> 0.
      rv_error = |Report { iv_report } not found|.
      RETURN.
    ENDIF.

    " Starting SA38/SE38 needs S_TCODE, which a SUBMIT never checks. Without
    " this, a user who can start neither could still run every report that
    " has no authorization group through the bridge.
    CALL FUNCTION 'AUTHORITY_CHECK_TCODE'
      EXPORTING
        tcode  = 'SA38'
      EXCEPTIONS
        ok     = 0
        not_ok = 1
        OTHERS = 2.
    IF sy-subrc <> 0.
      CALL FUNCTION 'AUTHORITY_CHECK_TCODE'
        EXPORTING
          tcode  = 'SE38'
        EXCEPTIONS
          ok     = 0
          not_ok = 1
          OTHERS = 2.
    ENDIF.
    IF sy-subrc <> 0.
      rv_error = |No authorization to run reports (S_TCODE SA38 or SE38)|.
      RETURN.
    ENDIF.

    IF lv_secu IS NOT INITIAL.
      AUTHORITY-CHECK OBJECT 'S_PROGRAM'
        ID 'P_GROUP'  FIELD lv_secu
        ID 'P_ACTION' FIELD 'SUBMIT'.
      IF sy-subrc = 0.
        AUTHORITY-CHECK OBJECT 'S_PROGRAM'
          ID 'P_GROUP'  FIELD lv_secu
          ID 'P_ACTION' FIELD 'BTCSUBMIT'.
      ENDIF.
      IF sy-subrc <> 0.
        rv_error = |No authorization to run report { iv_report }| &&
                   | (S_PROGRAM P_GROUP { lv_secu },| &&
                   | P_ACTION SUBMIT and BTCSUBMIT)|.
        RETURN.
      ENDIF.
    ENDIF.

    " Switchable allowlist (SACF) for generic report start; inert
    " unless the scenario is active.
    TRY.
        cl_sabe=>auth_check_progname(
          i_scenario_name = 'BC_GENERIC_REPORT_START'
          i_program_name  = iv_report
          i_action        = 'SUBMIT' ).
      CATCH cx_sabe.
        rv_error = |Report { iv_report } is not allowed for generic| &&
                   | start (S_PROGNAM, SACF BC_GENERIC_REPORT_START)|.
    ENDTRY.
  ENDMETHOD.


  METHOD check_develop_authority.
    " Repository changes made through the bridge bypass the workbench,
    " so check S_DEVELOP as SE80 would. The package defaults to the
    " object's current TADIR package; pass one to check a target package.
    DATA lv_secu TYPE trdir-secu.

    DATA(lv_devclass) = iv_devclass.
    IF lv_devclass IS INITIAL.
      SELECT SINGLE devclass FROM tadir
        WHERE pgmid    = 'R3TR'
          AND object   = @iv_object
          AND obj_name = @iv_obj_name
        INTO @lv_devclass.
      IF sy-subrc <> 0.
        rv_error = |Object { iv_object } { iv_obj_name } not found in TADIR|.
        RETURN.
      ENDIF.
    ENDIF.

    IF iv_object = 'PROG'.
      SELECT SINGLE secu FROM trdir
        WHERE name = @iv_obj_name
        INTO @lv_secu.
      AUTHORITY-CHECK OBJECT 'S_DEVELOP'
        ID 'DEVCLASS' FIELD lv_devclass
        ID 'OBJTYPE'  FIELD iv_object
        ID 'OBJNAME'  FIELD iv_obj_name
        ID 'P_GROUP'  FIELD lv_secu
        ID 'ACTVT'    FIELD iv_actvt.
    ELSE.
      AUTHORITY-CHECK OBJECT 'S_DEVELOP'
        ID 'DEVCLASS' FIELD lv_devclass
        ID 'OBJTYPE'  FIELD iv_object
        ID 'OBJNAME'  FIELD iv_obj_name
        ID 'P_GROUP'  DUMMY
        ID 'ACTVT'    FIELD iv_actvt.
    ENDIF.
    IF sy-subrc <> 0.
      rv_error = |No authorization for { iv_object } { iv_obj_name }| &&
                 | (S_DEVELOP ACTVT { iv_actvt }, package { lv_devclass })|.
    ENDIF.
  ENDMETHOD.

ENDCLASS.
