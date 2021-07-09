$(document).ready(function () {
    // enable bootstrap tooltips
    $('[data-toggle="tooltip"]').tooltip();

    let now = new Date();
    $("#last_update").html(now.toLocaleString());
});
