var temperatureIDs=[];
var gauges = {}
var updateInterval = 3000; //3 seconds
var tempMin = 0.0;
var tempMax = 0.0;
var tempTarget = 0.0;
var probeInterval = 0;
var recordInterval = 0;


function initHandler() {
	//get the list of temeratures and populate globals
	$.getJSON( "/api/config", function( data ) {
		tempMax = data.HighTemp;
		tempMin = data.LowTemp;
		tempTarget = data.TargetTemp;
		probeInterval = data.ProbeInterval;
		recordInterval = data.RecordInterval;
		$.each(data.Probes, function(k, v) {
			temperatureIDs.push(v);
		});
		//build out gauges
		buildGauges();
		//kick off an update
		tempUpdate();
	}).fail(function(error) {
		console.log("Failed to get config");
		console.log(error);
	});
}

function buildGauges() {
	min = tempMin;
	max = tempMax;
	$.each(temperatureIDs, function(k, v){
		divID=v.ID
		nd = '<div class="col-sx-12 col-sm-6 col-md-3 col-lg-3" align="center">' +
		'<div id="'+ divID +'gauge"></div>' +
		'<h4>'+v.Alias+'</h4>' +
		'<span class="text-muted" id="'+ v.ID +'temp">0.0 &deg;</span>'
		'</div>';
		$("#gauges").append(nd);
		if (!(v.MinOverride == 0.0 && v.MaxOverride == 0.0)) {
			min = v.MinOverride;
			max = v.MaxOverride;
		}
		var g = new JustGage({
			id: v.ID+'gauge',
			value: 0,
			min: min,
			max: max,
			title: v.Alias+" Temperature",
			levelColors: ["#ff0000", "#ffff00", "#00ff00", "#ffff00", "#ff0000"],
			valueFontColor: "#FFFFFF"
		});
		gauges[v.ID] = g
	});
}

function updateKegTemp(ID, value) {
	if (ID in gauges) {
		gauges[ID].refresh(value);
		$('#'+ID+'temp').html(value + " &deg; C");
	} else {
		console.log("Could not find any record of "+ID);
		console.log(gauges);
	}
}

function tempUpdate() {
	$.getJSON( "/api/temps/now", function( data ) {
		$.each(data, function(k, v) {
			updateKegTemp(k, v);
		});
	}).fail(function(error) {
		console.log("Failed to update temps "+error);
	});

	$.getJSON( "/api/compressor/now", function(data) {
		updateCompressorState(data);
	}).fail(function(error) {
		console.log("Failed to update compressor state "+error);
	});

	//do this last so if it bombs we don't just keep hitting the backend
	setTimeout(tempUpdate, updateInterval);
}

function updateCompressorState(state) {
	var s = "OFF"
	var color = "#ff0000"

	if(state == true) {
		s = "ON"
		color = "#00ff00"
	}
	$('#compressor_state').html(s)
	$('#compressor_state').attr('color', color)
}

function buildLogGraphs() {

}

function updateLogGraphs() {

}
