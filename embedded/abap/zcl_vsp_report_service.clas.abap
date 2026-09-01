class ZCL_VSP_REPORT_SERVICE definition
  public
  final
  create public .

public section.

  interfaces ZIF_VSP_SERVICE .

  " Hand-off record between the APC session and ZVSP_RUN_CAPTURE (INDX area ZV).
  " Both sides IMPORT/EXPORT this exact type, so it lives here and the report
  " references it - do not duplicate it.
  types:
    BEGIN OF ty_capture_instr,
      report  TYPE progname,
      variant TYPE variant,
      selpar  TYPE rsparams_tt,
    END OF ty_capture_instr .
protected section.
private section.

  methods HANDLE_RUN_REPORT
    importing
      !IS_MESSAGE type ZIF_VSP_SERVICE=>TY_MESSAGE
    returning
      value(RS_RESPONSE) type ZIF_VSP_SERVICE=>TY_RESPONSE .
  methods HANDLE_GET_JOB_STATUS
    importing
      !IS_MESSAGE type ZIF_VSP_SERVICE=>TY_MESSAGE
    returning
      value(RS_RESPONSE) type ZIF_VSP_SERVICE=>TY_RESPONSE .
  methods HANDLE_GET_SPOOL_OUTPUT
    importing
      !IS_MESSAGE type ZIF_VSP_SERVICE=>TY_MESSAGE
    returning
      value(RS_RESPONSE) type ZIF_VSP_SERVICE=>TY_RESPONSE .
  methods HANDLE_GET_TEXT_ELEMENTS
    importing
      !IS_MESSAGE type ZIF_VSP_SERVICE=>TY_MESSAGE
    returning
      value(RS_RESPONSE) type ZIF_VSP_SERVICE=>TY_RESPONSE .
  methods HANDLE_SET_TEXT_ELEMENTS
    importing
      !IS_MESSAGE type ZIF_VSP_SERVICE=>TY_MESSAGE
    returning
      value(RS_RESPONSE) type ZIF_VSP_SERVICE=>TY_RESPONSE .
  methods HANDLE_GET_VARIANTS
    importing
      !IS_MESSAGE type ZIF_VSP_SERVICE=>TY_MESSAGE
    returning
      value(RS_RESPONSE) type ZIF_VSP_SERVICE=>TY_RESPONSE .
  methods EXTRACT_PARAM
    importing
      !IV_PARAMS type STRING
      !IV_NAME type STRING
    returning
      value(RV_VALUE) type STRING .
  methods EXTRACT_PARAM_OBJECT
    importing
      !IV_PARAMS type STRING
      !IV_NAME type STRING
    returning
      value(RV_JSON) type STRING .
  methods ESCAPE_JSON
    importing
      !IV_STRING type STRING
    returning
      value(RV_ESCAPED) type STRING .
  methods BUILD_ERROR
    importing
      !IV_ID type STRING
      !IV_CODE type STRING
      !IV_MESSAGE type STRING
    returning
      value(RS_RESPONSE) type ZIF_VSP_SERVICE=>TY_RESPONSE .
ENDCLASS.



CLASS ZCL_VSP_REPORT_SERVICE IMPLEMENTATION.


  METHOD build_error.
    DATA(lv_o) = '{'.
    DATA(lv_c) = '}'.
    rs_response = VALUE #(
      id      = iv_id
      success = abap_false
      error   = |{ lv_o }"code":"{ iv_code }","message":"{ escape_json( iv_message ) }"{ lv_c }|
    ).
  ENDMETHOD.


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

    DATA lv_search TYPE string.
    lv_search = |"{ lv_name }":|.
    DATA lv_pos TYPE i.
    FIND lv_search IN iv_params MATCH OFFSET lv_pos.
    IF sy-subrc = 0.
      DATA lv_rest TYPE string.
      lv_rest = iv_params+lv_pos.
      FIND REGEX ':\s*"([^"]*)"' IN lv_rest SUBMATCHES rv_value.
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


  METHOD handle_get_job_status.
    DATA: lv_jobname  TYPE tbtcjob-jobname,
          lv_jobcount TYPE tbtcjob-jobcount,
          lv_key      TYPE indx-srtfd.

    DATA(lv_jobname_str) = extract_param( iv_params = is_message-params iv_name = 'jobname' ).
    DATA(lv_jobcount_str) = extract_param( iv_params = is_message-params iv_name = 'jobcount' ).

    IF lv_jobname_str IS INITIAL OR lv_jobcount_str IS INITIAL.
      rs_response = build_error( iv_id = is_message-id iv_code = 'MISSING_PARAM' iv_message = 'jobname and jobcount are required' ).
      RETURN.
    ENDIF.

    lv_jobname = lv_jobname_str.
    lv_jobcount = lv_jobcount_str.

    SELECT SINGLE status, sdldate FROM tbtco INTO ( @DATA(lv_status), @DATA(lv_sdldate) )
      WHERE jobname = @lv_jobname AND jobcount = @lv_jobcount.
    IF sy-subrc <> 0.
      rs_response = build_error( iv_id = is_message-id iv_code = 'JOB_NOT_FOUND' iv_message = |Job { lv_jobname }/{ lv_jobcount } not found| ).
      RETURN.
    ENDIF.

    " TBTCO status: P=planned S=released Y=ready R=active F=finished A=aborted
    DATA lv_status_txt TYPE string.
    CASE lv_status.
      WHEN 'F'.
        lv_status_txt = 'finished'.
      WHEN 'A'.
        lv_status_txt = 'aborted'.
      WHEN 'R' OR 'Y'.
        lv_status_txt = 'running'.
      WHEN OTHERS.
        lv_status_txt = 'scheduled'.
    ENDCASE.

    " A job run through ZVSP_RUN_CAPTURE has its list parked in INDX under
    " VSPO<jobcount><sdldate>; that key is handed back as the one "spool id",
    " because a real TemSe spool cannot be read from inside the APC session
    " (RSPO_RETURN_ABAP_SPOOLJOB SUBMITs -> APC_ILLEGAL_STATEMENT).
    DATA lv_spools TYPE string.
    lv_key = |VSPO{ lv_jobcount }{ lv_sdldate }|.
    SELECT SINGLE srtfd FROM indx INTO @DATA(lv_found)
      WHERE relid = 'ZV' AND srtfd = @lv_key AND srtf2 = 0.
    IF sy-subrc = 0.
      lv_spools = |"{ lv_key }"|.
    ELSE.
      " legacy: jobs not scheduled through the capture wrapper
      SELECT listident FROM tbtcp INTO TABLE @DATA(lt_spool)
        WHERE jobname = @lv_jobname AND jobcount = @lv_jobcount.
      LOOP AT lt_spool INTO DATA(lv_listident).
        DATA lv_id TYPE string.
        lv_id = lv_listident.
        SHIFT lv_id LEFT DELETING LEADING '0'.
        CONDENSE lv_id.
        IF lv_id IS INITIAL.
          CONTINUE.
        ENDIF.
        IF lv_spools IS NOT INITIAL.
          lv_spools = |{ lv_spools },|.
        ENDIF.
        lv_spools = |{ lv_spools }"{ lv_id }"|.
      ENDLOOP.
    ENDIF.

    DATA(lv_o) = '{'.
    DATA(lv_c) = '}'.
    rs_response = VALUE #(
      id      = is_message-id
      success = abap_true
      data    = |{ lv_o }"jobname":"{ lv_jobname }","jobcount":"{ lv_jobcount }","status":"{ lv_status_txt }","spool_ids":[{ lv_spools }]{ lv_c }|
    ).
  ENDMETHOD.


  METHOD handle_get_spool_output.
    " Only capture keys (VSPO<jobcount><sdldate>, written by ZVSP_RUN_CAPTURE)
    " can be served from here. A numeric TemSe spool id cannot: both
    " RSPO_RETURN_ABAP_SPOOLJOB and RSPO_RETURN_SPOOLJOB SUBMIT rspolist/rspolst2,
    " which is APC_ILLEGAL_STATEMENT in this session (verified ZED+ZES 2026-09-01).
    DATA: lv_key    TYPE indx-srtfd,
          lv_report TYPE progname,
          lt_buffer TYPE TABLE OF soli.

    DATA(lv_spool_str) = extract_param( iv_params = is_message-params iv_name = 'spool_id' ).

    IF lv_spool_str IS INITIAL.
      rs_response = build_error( iv_id = is_message-id iv_code = 'MISSING_PARAM' iv_message = 'spool_id is required' ).
      RETURN.
    ENDIF.

    IF strlen( lv_spool_str ) < 5 OR lv_spool_str(4) <> 'VSPO'.
      rs_response = build_error( iv_id = is_message-id iv_code = 'SPOOL_READ_UNSUPPORTED'
        iv_message = |Spool { lv_spool_str } is a TemSe spool; reading it needs SUBMIT, which is illegal in the APC session. Reports run through ZVSP_RUN_CAPTURE are served from INDX instead.| ).
      RETURN.
    ENDIF.

    lv_key = lv_spool_str.
    IMPORT report = lv_report lines = lt_buffer FROM DATABASE indx(zv) ID lv_key.
    IF sy-subrc <> 0.
      rs_response = build_error( iv_id = is_message-id iv_code = 'CAPTURE_NOT_FOUND' iv_message = |No captured list under { lv_key }| ).
      RETURN.
    ENDIF.
    " one read per capture; ZVSP_RUN_CAPTURE also sweeps anything older than 2 days
    DELETE FROM DATABASE indx(zv) ID lv_key.

    DATA lv_output TYPE string.
    LOOP AT lt_buffer INTO DATA(ls_line).
      IF sy-tabix > 1.
        lv_output = lv_output && cl_abap_char_utilities=>newline.
      ENDIF.
      lv_output = lv_output && ls_line-line.
    ENDLOOP.

    DATA(lv_lines) = lines( lt_buffer ).
    DATA(lv_o) = '{'.
    DATA(lv_c) = '}'.
    rs_response = VALUE #(
      id      = is_message-id
      success = abap_true
      data    = |{ lv_o }"spool_id":"{ lv_spool_str }","report":"{ lv_report }","lines":{ lv_lines },"output":"{ escape_json( lv_output ) }"{ lv_c }|
    ).
  ENDMETHOD.


  METHOD handle_get_text_elements.
    DATA: lt_textpool TYPE TABLE OF textpool,
          lv_program  TYPE progname.

    DATA(lv_prog_str) = extract_param( iv_params = is_message-params iv_name = 'program' ).
    DATA(lv_language) = extract_param( iv_params = is_message-params iv_name = 'language' ).

    IF lv_prog_str IS INITIAL.
      rs_response = build_error( iv_id = is_message-id iv_code = 'MISSING_PARAM' iv_message = 'Parameter program is required' ).
      RETURN.
    ENDIF.

    TRANSLATE lv_prog_str TO UPPER CASE.
    lv_program = lv_prog_str.

    DATA lv_lang TYPE sy-langu.
    IF lv_language IS NOT INITIAL.
      lv_lang = lv_language(1).
    ELSE.
      lv_lang = sy-langu.
    ENDIF.

    READ TEXTPOOL lv_program INTO lt_textpool LANGUAGE lv_lang.

    DATA(lv_o) = '{'.
    DATA(lv_c) = '}'.
    DATA lv_json TYPE string.
    lv_json = |{ lv_o }"program":"{ lv_program }","language":"{ lv_lang }","selection_texts":{ lv_o }|.

    DATA lv_first TYPE abap_bool VALUE abap_true.
    DATA lv_entry_str TYPE string.
    LOOP AT lt_textpool INTO DATA(ls_text) WHERE id = 'S'.
      IF lv_first = abap_false.
        lv_json = |{ lv_json },|.
      ENDIF.
      DATA(lv_key) = ls_text-key.
      CONDENSE lv_key.
      " Selection text entry has 8-char key prefix - strip it
      lv_entry_str = ls_text-entry.
      IF strlen( lv_entry_str ) > 8.
        lv_entry_str = lv_entry_str+8.
      ENDIF.
      lv_json = |{ lv_json }"{ lv_key }":"{ escape_json( lv_entry_str ) }"|.
      lv_first = abap_false.
    ENDLOOP.

    lv_json = |{ lv_json }{ lv_c },"text_symbols":{ lv_o }|.

    lv_first = abap_true.
    LOOP AT lt_textpool INTO ls_text WHERE id = 'I'.
      IF lv_first = abap_false.
        lv_json = |{ lv_json },|.
      ENDIF.
      lv_key = ls_text-key.
      CONDENSE lv_key.
      lv_entry_str = ls_text-entry.
      lv_json = |{ lv_json }"{ lv_key }":"{ escape_json( lv_entry_str ) }"|.
      lv_first = abap_false.
    ENDLOOP.

    lv_json = |{ lv_json }{ lv_c }{ lv_c }|.
    rs_response = VALUE #( id = is_message-id success = abap_true data = lv_json ).
  ENDMETHOD.


  METHOD handle_get_variants.
    DATA: lt_varid   TYPE TABLE OF varid,
          lv_report  TYPE progname.

    DATA(lv_report_str) = extract_param( iv_params = is_message-params iv_name = 'report' ).

    IF lv_report_str IS INITIAL.
      rs_response = build_error( iv_id = is_message-id iv_code = 'MISSING_PARAM' iv_message = 'Parameter report is required' ).
      RETURN.
    ENDIF.

    TRANSLATE lv_report_str TO UPPER CASE.
    lv_report = lv_report_str.

    SELECT * FROM varid INTO TABLE lt_varid
      WHERE report = lv_report
      ORDER BY variant.

    DATA(lv_o) = '{'.
    DATA(lv_c) = '}'.
    DATA lv_json TYPE string.
    lv_json = |{ lv_o }"report":"{ lv_report }","variants":[|.

    DATA lv_first TYPE abap_bool VALUE abap_true.
    LOOP AT lt_varid INTO DATA(ls_var).
      IF lv_first = abap_false.
        lv_json = |{ lv_json },|.
      ENDIF.
      DATA(lv_protected) = COND string( WHEN ls_var-protected = abap_true THEN 'true' ELSE 'false' ).
      lv_json = |{ lv_json }{ lv_o }"name":"{ ls_var-variant }","protected":{ lv_protected }{ lv_c }|.
      lv_first = abap_false.
    ENDLOOP.

    lv_json = |{ lv_json }]{ lv_c }|.
    rs_response = VALUE #( id = is_message-id success = abap_true data = lv_json ).
  ENDMETHOD.


  METHOD handle_run_report.
    " Running a report from inside an APC handler: what is and is not legal.
    "
    " APC forbids any statement that tears down the cross-transaction context.
    " On kernel 754 / Basis 750 SP29 all of these dump with APC_ILLEGAL_STATEMENT
    " (each proven by its own short dump on ZES 050 / ZED 050):
    "
    "   SUBMIT ... VIA JOB ... AND RETURN            (2026-08-21, "SUBMIT_AND_RETURN")
    "   CALL FUNCTION ... DESTINATION 'NONE'         (2026-08-21, "CALL FUNCTION .. DESTINATION")
    "   RSPO_RETURN_ABAP_SPOOLJOB (SUBMIT rspolist)  (2026-09-01, in SAPLSPOX)
    "
    " So neither running the report nor reading its spool can happen here.
    " Both are legal in a background job. This method therefore only:
    "   1. JOB_OPEN
    "   2. parks the instruction (report, variant, selection table) in INDX
    "      area ZV under VSPI<jobcount><sdldate>
    "   3. JOB_SUBMIT the wrapper ZVSP_RUN_CAPTURE as the step, JOB_CLOSE
    " The wrapper SUBMITs the target EXPORTING LIST TO MEMORY, stores the
    " ASCII lines under VSPO<jobcount><sdldate>, and handle_get_spool_output
    " reads them back. No variant is created any more; the selection table
    " travels with the instruction.
    " Rewritten by EDDU 2026-09-01 (JOB_SUBMIT-only scheduling was 2026-08-21).
    DATA: lt_selpar   TYPE rsparams_tt,
          lv_report   TYPE progname,
          lv_variant  TYPE variant,
          lv_jobname  TYPE tbtcjob-jobname,
          lv_jobcount TYPE tbtcjob-jobcount,
          lv_key      TYPE indx-srtfd,
          ls_indx     TYPE indx,
          ls_instr    TYPE ty_capture_instr.

    DATA(lv_report_str) = extract_param( iv_params = is_message-params iv_name = 'report' ).
    DATA(lv_variant_str) = extract_param( iv_params = is_message-params iv_name = 'variant' ).
    DATA(lv_params_json) = extract_param_object( iv_params = is_message-params iv_name = 'params' ).

    IF lv_report_str IS INITIAL.
      rs_response = build_error( iv_id = is_message-id iv_code = 'MISSING_PARAM' iv_message = 'Parameter report is required' ).
      RETURN.
    ENDIF.

    TRANSLATE lv_report_str TO UPPER CASE.
    lv_report = lv_report_str.
    IF lv_variant_str IS NOT INITIAL.
      TRANSLATE lv_variant_str TO UPPER CASE.
      lv_variant = lv_variant_str.
    ENDIF.

    SELECT SINGLE name FROM trdir INTO @DATA(lv_exists) WHERE name = @lv_report.
    IF sy-subrc <> 0.
      rs_response = build_error( iv_id = is_message-id iv_code = 'REPORT_NOT_FOUND' iv_message = |Report { lv_report } not found| ).
      RETURN.
    ENDIF.

    " optional selection params {"P_NAME":"value",...} -> RSPARAMS rows;
    " the wrapper seeds the correct KIND (P/S) from the report's own screen.
    IF lv_params_json IS NOT INITIAL.
      DATA(lv_work) = lv_params_json.
      WHILE lv_work CS '"'.
        DATA lv_pname TYPE string.
        DATA lv_pval  TYPE string.
        FIND REGEX '"([^"]+)"\s*:\s*"([^"]*)"' IN lv_work SUBMATCHES lv_pname lv_pval.
        IF sy-subrc <> 0.
          EXIT.
        ENDIF.
        TRANSLATE lv_pname TO UPPER CASE.
        APPEND VALUE rsparams( selname = lv_pname sign = 'I' option = 'EQ' low = lv_pval ) TO lt_selpar.
        FIND FIRST OCCURRENCE OF |"{ lv_pname }"| IN lv_work MATCH OFFSET DATA(lv_off) MATCH LENGTH DATA(lv_len) IGNORING CASE.
        IF sy-subrc = 0 AND strlen( lv_work ) > lv_off + lv_len.
          lv_work = lv_work+lv_off.
          lv_work = lv_work+lv_len.
        ELSE.
          EXIT.
        ENDIF.
      ENDWHILE.
    ENDIF.

    lv_jobname = |ZVSP_{ lv_report }|.

    CALL FUNCTION 'JOB_OPEN'
      EXPORTING
        jobname          = lv_jobname
      IMPORTING
        jobcount         = lv_jobcount
      EXCEPTIONS
        cant_create_job  = 1
        invalid_job_data = 2
        jobname_missing  = 3
        OTHERS           = 4.
    IF sy-subrc <> 0.
      rs_response = build_error( iv_id = is_message-id iv_code = 'JOB_OPEN_FAILED' iv_message = |JOB_OPEN failed (rc={ sy-subrc })| ).
      RETURN.
    ENDIF.

    " instruction for the wrapper; JOB_OPEN stamps TBTCO-SDLDATE = sy-datum,
    " which is what the wrapper uses to rebuild this key
    lv_key = |VSPI{ lv_jobcount }{ sy-datum }|.
    ls_instr = VALUE #( report = lv_report variant = lv_variant selpar = lt_selpar ).
    ls_indx-aedat = sy-datum.
    ls_indx-usera = sy-uname.
    ls_indx-pgmid = 'ZCL_VSP_REPORT_SERVICE'.
    EXPORT instr = ls_instr TO DATABASE indx(zv) FROM ls_indx ID lv_key.

    " JOB_SUBMIT is an ordinary function call - legal here, unlike SUBMIT.
    CALL FUNCTION 'JOB_SUBMIT'
      EXPORTING
        authcknam               = sy-uname
        jobcount                = lv_jobcount
        jobname                 = lv_jobname
        report                  = 'ZVSP_RUN_CAPTURE'
      EXCEPTIONS
        bad_priparams           = 1
        bad_xpgflags            = 2
        invalid_jobdata         = 3
        jobname_missing         = 4
        job_notex               = 5
        job_submit_failed       = 6
        lock_failed             = 7
        program_missing         = 8
        prog_abap_and_extpg_set = 9
        OTHERS                  = 10.
    IF sy-subrc <> 0.
      DELETE FROM DATABASE indx(zv) ID lv_key.
      rs_response = build_error( iv_id = is_message-id iv_code = 'JOB_SUBMIT_FAILED' iv_message = |JOB_SUBMIT of ZVSP_RUN_CAPTURE for { lv_report } failed (rc={ sy-subrc })| ).
      RETURN.
    ENDIF.

    CALL FUNCTION 'JOB_CLOSE'
      EXPORTING
        jobname              = lv_jobname
        jobcount             = lv_jobcount
        strtimmed            = 'X'
      EXCEPTIONS
        cant_start_immediate = 1
        invalid_startdate    = 2
        jobname_missing      = 3
        job_close_failed     = 4
        job_nosteps          = 5
        job_notex            = 6
        lock_failed          = 7
        OTHERS               = 8.
    IF sy-subrc <> 0.
      DELETE FROM DATABASE indx(zv) ID lv_key.
      rs_response = build_error( iv_id = is_message-id iv_code = 'JOB_CLOSE_FAILED' iv_message = |JOB_CLOSE failed (rc={ sy-subrc })| ).
      RETURN.
    ENDIF.

    DATA(lv_o) = '{'.
    DATA(lv_c) = '}'.
    rs_response = VALUE #(
      id      = is_message-id
      success = abap_true
      data    = |{ lv_o }"status":"scheduled","report":"{ lv_report }","jobname":"{ lv_jobname }","jobcount":"{ lv_jobcount }"{ lv_c }|
    ).
  ENDMETHOD.


  METHOD handle_set_text_elements.
    DATA: lt_textpool TYPE TABLE OF textpool,
          lv_program  TYPE progname.

    DATA(lv_prog_str) = extract_param( iv_params = is_message-params iv_name = 'program' ).
    DATA(lv_language) = extract_param( iv_params = is_message-params iv_name = 'language' ).
    DATA(lv_sel_json) = extract_param_object( iv_params = is_message-params iv_name = 'selection_texts' ).
    DATA(lv_sym_json) = extract_param_object( iv_params = is_message-params iv_name = 'text_symbols' ).

    IF lv_prog_str IS INITIAL.
      rs_response = build_error( iv_id = is_message-id iv_code = 'MISSING_PARAM' iv_message = 'Parameter program is required' ).
      RETURN.
    ENDIF.

    TRANSLATE lv_prog_str TO UPPER CASE.
    lv_program = lv_prog_str.

    DATA lv_lang TYPE sy-langu.
    IF lv_language IS NOT INITIAL.
      lv_lang = lv_language(1).
    ELSE.
      lv_lang = sy-langu.
    ENDIF.

    READ TEXTPOOL lv_program INTO lt_textpool LANGUAGE lv_lang.

    DATA lv_sel_count TYPE i.
    DATA lv_sym_count TYPE i.

    IF lv_sel_json IS NOT INITIAL.
      DATA(lv_work) = lv_sel_json.
      WHILE lv_work CS '"'.
        DATA lv_key TYPE string.
        DATA lv_val TYPE string.
        FIND REGEX '"([^"]+)"\s*:\s*"([^"]*)"' IN lv_work SUBMATCHES lv_key lv_val.
        IF sy-subrc = 0.
          TRANSLATE lv_key TO UPPER CASE.
          REPLACE ALL OCCURRENCES OF '\"' IN lv_val WITH '"'.
          REPLACE ALL OCCURRENCES OF '\\' IN lv_val WITH '\'.

          DATA lv_textkey TYPE textpoolky.
          lv_textkey = lv_key.
          " Selection text entry must be: 8-char key prefix + text value
          DATA(lv_entry) = |{ lv_textkey WIDTH = 8 }{ lv_val }|.
          READ TABLE lt_textpool ASSIGNING FIELD-SYMBOL(<fs>) WITH KEY id = 'S' key = lv_textkey.
          IF sy-subrc = 0.
            <fs>-entry = lv_entry.
          ELSE.
            APPEND VALUE textpool( id = 'S' key = lv_textkey entry = lv_entry ) TO lt_textpool.
          ENDIF.
          lv_sel_count = lv_sel_count + 1.

          FIND FIRST OCCURRENCE OF |"{ lv_key }"| IN lv_work MATCH OFFSET DATA(lv_off) MATCH LENGTH DATA(lv_len) IGNORING CASE.
          IF sy-subrc = 0 AND strlen( lv_work ) > lv_off + lv_len.
            lv_work = lv_work+lv_off.
            lv_work = lv_work+lv_len.
          ELSE.
            EXIT.
          ENDIF.
        ELSE.
          EXIT.
        ENDIF.
      ENDWHILE.
    ENDIF.

    IF lv_sym_json IS NOT INITIAL.
      lv_work = lv_sym_json.
      WHILE lv_work CS '"'.
        CLEAR: lv_key, lv_val.
        FIND REGEX '"([^"]+)"\s*:\s*"([^"]*)"' IN lv_work SUBMATCHES lv_key lv_val.
        IF sy-subrc = 0.
          REPLACE ALL OCCURRENCES OF '\"' IN lv_val WITH '"'.
          REPLACE ALL OCCURRENCES OF '\\' IN lv_val WITH '\'.

          lv_textkey = lv_key.
          READ TABLE lt_textpool ASSIGNING <fs> WITH KEY id = 'I' key = lv_textkey.
          IF sy-subrc = 0.
            <fs>-entry = lv_val.
          ELSE.
            APPEND VALUE textpool( id = 'I' key = lv_textkey entry = lv_val ) TO lt_textpool.
          ENDIF.
          lv_sym_count = lv_sym_count + 1.

          FIND FIRST OCCURRENCE OF |"{ lv_key }"| IN lv_work MATCH OFFSET lv_off MATCH LENGTH lv_len.
          IF sy-subrc = 0 AND strlen( lv_work ) > lv_off + lv_len.
            lv_work = lv_work+lv_off.
            lv_work = lv_work+lv_len.
          ELSE.
            EXIT.
          ENDIF.
        ELSE.
          EXIT.
        ENDIF.
      ENDWHILE.
    ENDIF.

    INSERT TEXTPOOL lv_program FROM lt_textpool LANGUAGE lv_lang.

    DATA(lv_o) = '{'.
    DATA(lv_c) = '}'.
    DATA(lv_status) = COND string( WHEN sy-subrc = 0 THEN 'success' ELSE 'error' ).
    DATA lv_json TYPE string.
    lv_json = |{ lv_o }"status":"{ lv_status }","program":"{ lv_program }","language":"{ lv_lang }","selection_texts_set":{ lv_sel_count },"text_symbols_set":{ lv_sym_count }{ lv_c }|.

    rs_response = VALUE #( id = is_message-id success = abap_true data = lv_json ).
  ENDMETHOD.


  METHOD zif_vsp_service~get_domain.
    rv_domain = 'report'.
  ENDMETHOD.


  METHOD zif_vsp_service~handle_message.
    CASE is_message-action.
      WHEN 'runReport'.
        rs_response = handle_run_report( is_message ).
      WHEN 'getJobStatus'.
        rs_response = handle_get_job_status( is_message ).
      WHEN 'getSpoolOutput'.
        rs_response = handle_get_spool_output( is_message ).
      WHEN 'getTextElements'.
        rs_response = handle_get_text_elements( is_message ).
      WHEN 'setTextElements'.
        rs_response = handle_set_text_elements( is_message ).
      WHEN 'getVariants'.
        rs_response = handle_get_variants( is_message ).
      WHEN OTHERS.
        rs_response = build_error(
          iv_id      = is_message-id
          iv_code    = 'UNKNOWN_ACTION'
          iv_message = |Action '{ is_message-action }' not supported|
        ).
    ENDCASE.
  ENDMETHOD.


  METHOD zif_vsp_service~on_disconnect.
  ENDMETHOD.
ENDCLASS.
