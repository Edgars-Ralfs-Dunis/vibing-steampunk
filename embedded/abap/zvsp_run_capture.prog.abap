*&---------------------------------------------------------------------*
*& Report ZVSP_RUN_CAPTURE - vsp bridge helper (package ZADT_VSP)
*&---------------------------------------------------------------------*
*& SUBMIT and TemSe spool reading are illegal inside the ZADT_VSP APC
*& session (APC_ILLEGAL_STATEMENT on kernel 754), but legal in a batch
*& job. ZCL_VSP_REPORT_SERVICE therefore schedules THIS report as the
*& job step after parking the instruction (report, variant, selection
*& table) in INDX area ZV under VSPI<jobcount><sdldate>.
*& This report SUBMITs the target EXPORTING LIST TO MEMORY, converts the
*& list to text, stores the lines under VSPO<jobcount><sdldate> for the
*& bridge (getSpoolOutput), and WRITEs them again so the job still has a
*& normal spool for SM37. No selection screen on purpose: JOB_SUBMIT then
*& needs no variant, and the job key alone locates the instruction.
*& EDDU 2026-09-01
*&---------------------------------------------------------------------*
REPORT zvsp_run_capture NO STANDARD PAGE HEADING LINE-SIZE 255.

DATA: gs_instr    TYPE zcl_vsp_report_service=>ty_capture_instr,
      gt_list     TYPE TABLE OF abaplist,
      gt_asci     TYPE TABLE OF soli,
      gt_skel     TYPE TABLE OF rsparams,
      gs_indx     TYPE indx,
      gv_jobname  TYPE tbtcjob-jobname,
      gv_jobcount TYPE tbtcjob-jobcount,
      gv_sdldate  TYPE tbtco-sdldate,
      gv_key_in   TYPE indx-srtfd,
      gv_key_out  TYPE indx-srtfd,
      gv_cutoff   TYPE d,
      gv_lines    TYPE string.

START-OF-SELECTION.

  CALL FUNCTION 'GET_JOB_RUNTIME_INFO'
    IMPORTING
      jobcount        = gv_jobcount
      jobname         = gv_jobname
    EXCEPTIONS
      no_runtime_info = 1
      OTHERS          = 2.
  IF sy-subrc <> 0 OR gv_jobcount IS INITIAL.
    MESSAGE e398(00) WITH 'ZVSP_RUN_CAPTURE only runs as a background job step'.
  ENDIF.

  SELECT SINGLE sdldate FROM tbtco
    WHERE jobname = @gv_jobname AND jobcount = @gv_jobcount
    INTO @gv_sdldate.
  gv_key_in  = |VSPI{ gv_jobcount }{ gv_sdldate }|.
  gv_key_out = |VSPO{ gv_jobcount }{ gv_sdldate }|.

  " the bridge exports the instruction between JOB_OPEN and JOB_CLOSE;
  " allow its commit a few seconds before giving up
  DO 6 TIMES.
    IMPORT instr = gs_instr FROM DATABASE indx(zv) ID gv_key_in.
    IF sy-subrc = 0.
      EXIT.
    ENDIF.
    WAIT UP TO 1 SECONDS.
  ENDDO.
  IF gs_instr-report IS INITIAL.
    MESSAGE e398(00) WITH 'ZVSP_RUN_CAPTURE: no instruction under' gv_key_in.
  ENDIF.
  DELETE FROM DATABASE indx(zv) ID gv_key_in.

  " housekeeping: captures nobody collected
  gv_cutoff = sy-datum - 2.
  DELETE FROM indx WHERE relid = 'ZV' AND aedat < @gv_cutoff.

  MESSAGE i398(00) WITH 'ZVSP_RUN_CAPTURE: running' gs_instr-report gs_instr-variant.

  " seed the target's real selection skeleton so every field gets its
  " correct KIND (P for PARAMETERS, S for SELECT-OPTIONS), then overlay
  " the caller's values by SELNAME
  IF gs_instr-selpar IS NOT INITIAL.
    CALL FUNCTION 'RS_REFRESH_FROM_SELECTOPTIONS'
      EXPORTING
        curr_report     = gs_instr-report
      TABLES
        selection_table = gt_skel
      EXCEPTIONS
        not_found       = 1
        no_report       = 2
        OTHERS          = 3.
    IF sy-subrc = 0.
      LOOP AT gs_instr-selpar INTO DATA(ls_in).
        READ TABLE gt_skel ASSIGNING FIELD-SYMBOL(<p>) WITH KEY selname = ls_in-selname.
        IF sy-subrc = 0.
          <p>-sign   = COND #( WHEN ls_in-sign IS NOT INITIAL THEN ls_in-sign ELSE 'I' ).
          <p>-option = COND #( WHEN ls_in-option IS NOT INITIAL THEN ls_in-option ELSE 'EQ' ).
          <p>-low    = ls_in-low.
          <p>-high   = ls_in-high.
        ENDIF.
      ENDLOOP.
    ELSE.
      gt_skel = gs_instr-selpar.
    ENDIF.
  ENDIF.

  IF gs_instr-variant IS NOT INITIAL AND gt_skel IS NOT INITIAL.
    SUBMIT (gs_instr-report)
      USING SELECTION-SET gs_instr-variant
      WITH SELECTION-TABLE gt_skel
      EXPORTING LIST TO MEMORY AND RETURN.
  ELSEIF gs_instr-variant IS NOT INITIAL.
    SUBMIT (gs_instr-report)
      USING SELECTION-SET gs_instr-variant
      EXPORTING LIST TO MEMORY AND RETURN.
  ELSEIF gt_skel IS NOT INITIAL.
    SUBMIT (gs_instr-report)
      WITH SELECTION-TABLE gt_skel
      EXPORTING LIST TO MEMORY AND RETURN.
  ELSE.
    SUBMIT (gs_instr-report)
      EXPORTING LIST TO MEMORY AND RETURN.
  ENDIF.

  CALL FUNCTION 'LIST_FROM_MEMORY'
    TABLES
      listobject = gt_list
    EXCEPTIONS
      not_found  = 1
      OTHERS     = 2.
  IF sy-subrc = 0.
    CALL FUNCTION 'LIST_TO_ASCI'
      TABLES
        listasci           = gt_asci
        listobject         = gt_list
      EXCEPTIONS
        empty_list         = 1
        list_index_invalid = 2
        OTHERS             = 3.
    CALL FUNCTION 'LIST_FREE_MEMORY'
      TABLES
        listobject = gt_list
      EXCEPTIONS
        OTHERS     = 1.
  ENDIF.

  gs_indx-aedat = sy-datum.
  gs_indx-usera = sy-uname.
  gs_indx-pgmid = sy-repid.
  EXPORT report = gs_instr-report lines = gt_asci
    TO DATABASE indx(zv) FROM gs_indx ID gv_key_out.
  COMMIT WORK.

  gv_lines = lines( gt_asci ).
  MESSAGE i398(00) WITH 'ZVSP_RUN_CAPTURE: captured' gv_lines 'lines under' gv_key_out.

  " keep a normal spool for SM37 readers
  LOOP AT gt_asci INTO DATA(ls_asci).
    WRITE: / ls_asci-line.
  ENDLOOP.
